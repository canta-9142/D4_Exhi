package app

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

var ErrStopped = errors.New("app has stopped")

type Options struct {
	// Send queues Message.MarshalLine() with LF framing, without waiting for I/O.
	// Callbacks must not synchronously call back into app.
	Send   func(SessionID, Message) error
	Close  func(SessionID)
	Logger *slog.Logger
	// Devices must have passed LoadDevices validation. SaveDevices is called
	// from Run before assignment changes become observable.
	Devices     []SavedDevice
	SaveDevices func([]SavedDevice) error
}

type session struct {
	uid            string
	failed         bool
	waitingHello   bool
	identification uint64
	sync           string
	lastRID        uint32
	hasRID         bool
	lastIdentify   time.Time
	lastStatus     StatusData
	hasStatus      bool
	forceStatus    bool
	phasePending   bool
	summaryPending bool
}

type device struct {
	kind    DeviceKind
	node    NodeID
	session SessionID
	noAuto  bool // Explicitly unassigned UIDs never regain a role automatically.
}

type request struct {
	apply   func(time.Time) error
	advance bool
	reply   chan error
}

// App owns all state in Run's goroutine. Use its methods from web and serial.
type App struct {
	game      game
	sessions  map[SessionID]*session
	devices   map[string]*device
	preview   *preview
	options   Options
	requests  chan request
	done      chan struct{}
	running   atomic.Bool
	now       func() time.Time
	ticks     <-chan time.Time
	eventNow  time.Time
	advancing bool
	fatal     error
}

func New(options Options) *App {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	a := &App{
		sessions: make(map[SessionID]*session), devices: make(map[string]*device),
		options: options, requests: make(chan request), done: make(chan struct{}), now: time.Now,
	}
	a.game = newGame(a.notify)
	for _, d := range options.Devices {
		a.devices[d.DeviceID] = &device{kind: d.Kind, node: d.Node, noAuto: d.AutoAssignExcluded}
	}
	return a
}

// Run blocks until ctx is canceled. An App can be run only once.
func (a *App) Run(ctx context.Context) error {
	if !a.running.CompareAndSwap(false, true) {
		return errors.New("app can only run once")
	}
	defer func() {
		for id := range a.sessions {
			a.closeSession(id)
		}
		close(a.done)
	}()
	ticks := a.ticks
	if ticks == nil {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case req := <-a.requests:
			a.eventNow = a.now()
			if req.advance {
				a.advance(a.eventNow)
			}
			if a.fatal != nil {
				return a.fatal
			}
			err := req.apply(a.eventNow)
			if req.advance {
				a.autoAssignPending()
				a.flushPlayerUpdates()
			}
			if a.fatal != nil {
				return a.fatal
			}
			req.reply <- err
		case <-ticks:
			a.eventNow = a.now()
			a.advance(a.eventNow)
			a.autoAssignPending()
			a.identifyPending(a.eventNow)
			a.flushPlayerUpdates()
			if a.fatal != nil {
				return a.fatal
			}
		}
	}
}

// Catch up using the operation modes that existed before this input. Transport
// failures stop writes immediately; their game effects begin at processing time.
func (a *App) advance(now time.Time) {
	a.advancing = true
	a.game.advance(now)
	a.advancing = false
	for id, s := range a.sessions {
		if s.failed {
			a.disconnect(id)
		}
	}
}

func (a *App) submit(ctx context.Context, advance bool, apply func(time.Time) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req := request{apply: apply, advance: advance, reply: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.done:
		return ErrStopped
	case a.requests <- req:
	}
	select {
	case err := <-req.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-a.done:
		return ErrStopped
	}
}

func (a *App) Execute(ctx context.Context, command Command) error {
	return a.submit(ctx, true, func(now time.Time) error {
		switch command.Operation {
		case Start:
			if err := a.game.startError(); err != nil {
				return err
			}
			a.restorePreview()
			return a.game.start(now)
		case Finish:
			return a.game.finish()
		case Reset:
			a.game.reset()
		case SetNodeEnabled:
			if a.game.phase == Playing {
				return reject(CodePhaseConflict, "operation mode cannot change while Playing")
			}
			if !command.Node.player() {
				return reject(CodeInvalid, "SetNodeEnabled requires a player node")
			}
			id := a.nodeSession(PlayerDevice, command.Node)
			if command.Enabled && id == "" {
				return reject(CodeDeviceDisconnected, "player MCU is not connected and identified")
			}
			if a.game.nodes[command.Node].manual != command.Enabled {
				if command.Enabled && a.preview != nil && a.preview.uid == a.sessions[id].uid {
					a.clearPreview()
				}
				a.game.setManual(command.Node, command.Enabled)
				if id != "" {
					a.syncDevice(id, true)
				}
			}
		case SetBotRate:
			if !command.Node.bot() || command.Rate <= 0 || math.IsNaN(command.Rate) || math.IsInf(command.Rate, 0) {
				return reject(CodeInvalid, "SetBotRate requires a bot node and a finite positive rate")
			}
			if a.game.nodes[command.Node].rate != command.Rate {
				a.game.nodes[command.Node].rate = command.Rate
				a.game.schedule(command.Node)
			}
		case SetDeviceAssignment, SwapDeviceAssignments, ReplaceDeviceAssignment:
			return a.changeAssignments(command)
		case SetDevicePreview:
			return a.setPreview(command.DeviceID)
		default:
			return reject(CodeInvalid, "unknown operation")
		}
		return nil
	})
}

func (a *App) State(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	err := a.submit(ctx, false, func(time.Time) error {
		g := &a.game
		reasons := g.startBlockers()
		snapshot = Snapshot{
			Phase: g.phase, Elapsed: g.elapsed, Remaining: GameDuration - g.elapsed, RemainingSeconds: a.remainingSeconds(),
			CanStart: len(reasons) == 0, StartBlockers: reasons,
			Nodes: make([]NodeSnapshot, 0, NodeCount), Devices: make([]DeviceSnapshot, 0, len(a.devices)),
		}
		for id := NodeID(1); id <= NodeCount; id++ {
			n := &g.nodes[id]
			kind := "player"
			if id.bot() {
				kind = "bot"
			}
			var rank *int
			if n.rank != 0 {
				value := n.rank
				rank = &value
			}
			snapshot.Nodes = append(snapshot.Nodes, NodeSnapshot{
				ID: id, Kind: kind, Enabled: n.manual, FIFOUsage: len(n.fifo), Score: n.score,
				RankingEligible: n.eligible, Rate: n.rate, Ready: n.ready, Rank: rank,
			})
		}
		for uid, d := range a.devices {
			assignmentKind := DeviceKind("")
			if d.node != 0 {
				assignmentKind = d.kind
			}
			snapshot.Devices = append(snapshot.Devices, DeviceSnapshot{
				ID: uid, Node: d.node, Kind: d.kind, AssignmentKind: assignmentKind, Session: d.session, Connected: d.session != "",
			})
		}
		slices.SortFunc(snapshot.Devices, func(a, b DeviceSnapshot) int { return strings.Compare(a.ID, b.ID) })
		if p := a.preview; p != nil {
			snapshot.Preview = &PreviewSnapshot{DeviceID: p.uid, Kind: a.devices[p.uid].kind, Node: p.node}
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (a *App) Connected(ctx context.Context, id SessionID) error {
	return a.submit(ctx, true, func(now time.Time) error {
		if id == "" {
			return errors.New("empty session ID")
		}
		if _, exists := a.sessions[id]; exists {
			return errors.New("session already connected")
		}
		a.sessions[id] = &session{}
		a.identify(id, now)
		return nil
	})
}

func (a *App) Disconnected(ctx context.Context, id SessionID) error {
	return a.submit(ctx, true, func(time.Time) error { a.disconnect(id); return nil })
}

func (a *App) MessageReceived(ctx context.Context, id SessionID, message IncomingMessage) error {
	return a.submit(ctx, true, func(time.Time) error {
		s := a.sessions[id]
		if s == nil || message.Identification == 0 || message.Identification != s.identification {
			return nil
		}
		validHello := message.Type == "hello" && validIdentity(message.Kind, message.UID)
		if s.waitingHello && !validHello {
			return nil
		}
		if message.Type == "hello" {
			return a.hello(id, message)
		}
		d := a.devices[s.uid]
		if d == nil || d.kind != PlayerDevice {
			a.closeSession(id)
			return errors.New("invalid message for device kind")
		}
		node := a.sessionNode(id)
		switch message.Type {
		case "resync":
			wasPreview := a.preview != nil && a.preview.uid == s.uid && d.session == id
			if wasPreview {
				a.clearPreview()
			}
			if node != 0 {
				a.syncDevice(id, wasPreview)
			} else if wasPreview {
				a.restoreUnassigned(s.uid)
			}
		case "ready":
			if !validHex(message.Sync, 32) {
				a.closeSession(id)
				return errors.New("invalid READY generation")
			}
			if node == 0 {
				return nil
			}
			if a.game.phase == Preparing && a.game.nodes[node].manual && s.sync != "" && s.sync == message.Sync {
				a.game.nodes[node].ready = true
			}
			s.forceStatus = true
		case "send":
			if !validHex(message.Sync, 32) || message.To < 1 || message.To > NodeCount {
				a.closeSession(id)
				return errors.New("invalid SEND fields")
			}
			if s.hasRID && message.RID <= s.lastRID {
				return nil
			}
			s.lastRID, s.hasRID = message.RID, true
			current := s.sync != "" && s.sync == message.Sync
			ok := current && a.game.canTransfer(node, message.PID, message.To)
			if ok {
				// Commit before sending; publish RESULT before the transfer's notifications.
				changes := a.game.transfer(node, message.To, true)
				a.send(id, Message{Type: "result", Data: ResultData{message.Sync, message.RID, true}})
				for _, c := range changes {
					a.notify(c)
				}
			} else if a.send(id, Message{Type: "result", Data: ResultData{message.Sync, message.RID, false}}) && current && node != 0 {
				a.resetFIFO(id, node)
			}
		default:
			a.closeSession(id)
			return errors.New("unknown MCU message")
		}
		return nil
	})
}

func validHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func validIdentity(kind DeviceKind, uid string) bool {
	return (kind == PlayerDevice || kind == LEDDevice) && validHex(uid, 12)
}

func (a *App) disconnect(id SessionID) {
	s := a.sessions[id]
	if s == nil {
		return
	}
	delete(a.sessions, id)
	if d := a.devices[s.uid]; d != nil && d.session == id {
		if a.preview != nil && a.preview.uid == s.uid {
			a.clearPreview()
		}
		d.session = ""
		if d.kind == PlayerDevice && d.node != 0 {
			a.game.setManual(d.node, false)
			a.game.nodes[d.node].ready = false
		}
	}
}

func (a *App) closeSession(id SessionID) {
	s := a.sessions[id]
	if s == nil || s.failed {
		return
	}
	s.failed = true
	if !a.advancing {
		a.disconnect(id)
	}
	if a.options.Close != nil {
		a.options.Close(id)
	}
}

func (a *App) send(id SessionID, message Message) bool {
	if a.fatal != nil {
		return false
	}
	if s := a.sessions[id]; s == nil || s.failed {
		return false
	}
	err := errors.New("serial transport is not configured")
	if a.options.Send != nil {
		err = a.options.Send(id, message)
	}
	if err == nil {
		return true
	}
	a.options.Logger.Warn("serial send failed", "session", id, "message", message.Type, "error", err)
	a.closeSession(id)
	return false
}

func (a *App) identify(id SessionID, now time.Time) {
	s := a.sessions[id]
	if s == nil {
		return
	}
	begin := !s.waitingHello
	if begin {
		if s.identification == ^uint64(0) {
			a.options.Logger.Error("identification generations exhausted", "session", id)
			a.closeSession(id)
			return
		}
		s.identification++
	}
	s.waitingHello, s.lastIdentify, s.sync, s.hasStatus = true, now, "", false
	s.phasePending, s.summaryPending, s.forceStatus = false, false, false
	a.send(id, Message{Type: "identify", Data: struct{}{}, BeginIdentification: begin, Identification: s.identification})
}

func (a *App) identifyPending(now time.Time) {
	for id, s := range a.sessions {
		if s.waitingHello && now.Sub(s.lastIdentify) >= time.Second {
			a.identify(id, now)
		}
	}
}
