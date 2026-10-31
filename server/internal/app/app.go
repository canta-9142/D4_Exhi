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
	// Devices must have passed LoadDevices validation.
	Devices []SavedDevice
	// SaveDevices synchronously persists the complete candidate before commit.
	// Nil allows in-memory operation. It must not call back into app.
	SaveDevices func([]SavedDevice) error
	// Records are the saved games in save order, already validated by the
	// records package. They are ignored if RecordsUnavailable is set.
	Records []GameRecord
	// SaveGame synchronously persists a finished game once, at the transition
	// to Finished. Nil allows in-memory operation. It must not call back into
	// app.
	SaveGame func(GameRecord) error
	// RecordsUnavailable is why the records file could not be used at
	// startup. If set, app never calls SaveGame and counts only the games of
	// this process.
	RecordsUnavailable string
	// Mode is the game mode, fixed for the run. Empty means Endless.
	Mode GameMode
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
	lastStatus     statusKey
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
	game          game
	sessions      map[SessionID]*session
	devices       map[string]*device
	preview       *preview
	options       Options
	requests      chan request
	done          chan struct{}
	running       atomic.Bool
	now           func() time.Time
	ticks         <-chan time.Time
	eventNow      time.Time
	advancing     bool
	fatalErr      error
	assignPending bool
	bests         bests
	gameBest      *BestScore    // The finished simultaneous game's best until Reset.
	recentRounds  []RecentRound // Newest first, at most maxRecentRounds.
	saveFailed    bool          // Whether the latest SaveGame failed.
}

func New(options Options) *App {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	a := &App{
		sessions: make(map[SessionID]*session), devices: make(map[string]*device),
		options: options, requests: make(chan request), done: make(chan struct{}), now: time.Now,
	}
	a.game = newGame(options.Mode, a.notify)
	for _, d := range options.Devices {
		a.devices[d.DeviceID] = &device{kind: d.Kind, node: d.Node, noAuto: d.AutoAssignExcluded}
	}
	if options.RecordsUnavailable == "" {
		for _, r := range options.Records {
			a.bests.add(r)
		}
	}
	return a
}

// Run blocks until ctx is canceled or persistence becomes uncertain.
// An App can be run only once.
func (a *App) Run(ctx context.Context) error {
	if !a.running.CompareAndSwap(false, true) {
		return errors.New("app can only run once")
	}
	defer func() {
		for id := range a.sessions {
			if a.fatalErr != nil {
				if !a.sessions[id].failed && a.options.Close != nil {
					a.options.Close(id)
				}
			} else {
				a.closeSession(id)
			}
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
			if a.fatalErr != nil {
				return a.fatalErr
			}
			err := req.apply(a.eventNow)
			if a.fatalErr != nil {
				return a.fatalErr
			}
			if req.advance {
				a.autoAssignPending()
				if a.fatalErr != nil {
					return a.fatalErr
				}
				a.flushPlayerUpdates()
			}
			req.reply <- err
		case <-ticks:
			a.eventNow = a.now()
			a.advance(a.eventNow)
			if a.fatalErr != nil {
				return a.fatalErr
			}
			a.autoAssignPending()
			if a.fatalErr != nil {
				return a.fatalErr
			}
			a.identifyPending(a.eventNow)
			a.flushPlayerUpdates()
		}
	}
}

// Catch up using the operation modes that existed before this input. Transport
// failures stop writes immediately; their game effects begin at processing time.
func (a *App) advance(now time.Time) {
	a.advancing = true
	a.game.advance(now)
	a.advancing = false
	if a.fatalErr != nil {
		return
	}
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

// Execute runs command and reports only whether it was accepted.
func (a *App) Execute(ctx context.Context, command Command) error {
	_, err := a.ExecuteWithResult(ctx, command)
	return err
}

// ExecuteWithResult runs command like Execute and also returns what the
// operation produced. Only SetAllNodesEnabled has a result.
func (a *App) ExecuteWithResult(ctx context.Context, command Command) (Result, error) {
	var result Result
	err := a.submit(ctx, true, func(now time.Time) error {
		switch command.Operation {
		case Start:
			// Start ends any preview while it resets every MCU (server.md 4.2.2).
			return a.game.start(now)
		case Stop:
			return a.game.stop()
		case Finish:
			return a.game.finish(FinishManual)
		case Reset:
			return a.game.reset()
		case ReturnToBot:
			if a.game.mode != Endless {
				return reject(CodePhaseConflict, "ReturnToBot requires endless operation")
			}
			if !command.Node.player() {
				return reject(CodeInvalid, "ReturnToBot requires a player node")
			}
			return a.game.returnToBot(command.Node)
		case SetNodeEnabled:
			if a.game.locked() {
				return reject(CodePhaseConflict, "operation mode cannot change while Playing")
			}
			if !command.Node.player() {
				return reject(CodeInvalid, "SetNodeEnabled requires a player node")
			}
			id := a.nodeSession(PlayerDevice, command.Node)
			if command.Enabled && id == "" {
				return reject(CodeDeviceDisconnected, "player MCU is not connected and identified")
			}
			a.applyNodeEnabled(command.Node, command.Enabled, id)
		case SetAllNodesEnabled:
			if a.game.locked() {
				return reject(CodePhaseConflict, "operation mode cannot change while Playing")
			}
			// A node without an identified MCU cannot become manual; it stays
			// automatic and is reported instead of rejecting the others.
			for node := NodeID(1); node <= PlayerCount; node++ {
				id := a.nodeSession(PlayerDevice, node)
				if command.Enabled && id == "" {
					a.applyNodeEnabled(node, false, id)
					result.Skipped = append(result.Skipped, node)
					continue
				}
				a.applyNodeEnabled(node, command.Enabled, id)
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
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// applyNodeEnabled sets the operation mode of a player node whose connected
// and identified MCU is session id ("" when there is none), and brings that
// MCU in line. Setting the current mode changes nothing.
func (a *App) applyNodeEnabled(node NodeID, enabled bool, id SessionID) {
	if a.game.nodes[node].manual == enabled {
		return
	}
	if enabled && a.preview != nil && a.preview.uid == a.sessions[id].uid {
		a.clearPreview()
	}
	a.game.setManual(node, enabled)
	if id != "" {
		a.syncDevice(id, true)
	}
}

func (a *App) State(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	err := a.submit(ctx, false, func(time.Time) error {
		g := &a.game
		reasons := g.startBlockers()
		snapshot = Snapshot{
			Mode: g.mode, Operation: OperationRunning, Elapsed: g.elapsed,
			CanStart: len(reasons) == 0, StartBlockers: reasons,
			Nodes: make([]NodeSnapshot, 0, NodeCount), Devices: make([]DeviceSnapshot, 0, len(a.devices)),
		}
		if g.mode == Simultaneous {
			snapshot.Phase, snapshot.Remaining = g.phase, GameDuration-g.elapsed
			snapshot.RemainingSeconds, snapshot.CountdownSeconds = a.remainingSeconds(), g.countdown
		} else if !g.running {
			snapshot.Operation = OperationStopped
		}
		for id := NodeID(1); id <= NodeCount; id++ {
			n := &g.nodes[id]
			node := NodeSnapshot{ID: id, Kind: "bot", FIFOUsage: len(n.fifo), Rate: n.rate}
			if id.player() {
				v := g.view(id)
				node.Kind, node.Enabled, node.Score, node.TopPercent = "player", n.manual, v.score, optionalPercent(n.topPercent)
				node.RankingEligible, node.Ready = n.eligible, v.ready
				if n.rank != 0 {
					rank := n.rank
					node.Rank = &rank
				}
				node.Station, node.Round = v.station, v.round
				node.CountdownSeconds, node.RemainingSeconds, node.WindowSeconds = v.countdown, v.remaining, v.window
			}
			snapshot.Nodes = append(snapshot.Nodes, node)
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
		snapshot.Records = a.recordsSnapshot()
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
			// Accepted or not, the MCU gets the current status (server.md 4.7).
			if s.sync != "" && s.sync == message.Sync {
				a.game.ready(node)
			}
			s.forceStatus = true
		case "choose":
			round := RoundKind(message.Round)
			if !validHex(message.Sync, 32) || round != RoundNormal && round != RoundTutorial {
				a.closeSession(id)
				return errors.New("invalid CHOOSE fields")
			}
			if node == 0 {
				return nil
			}
			if s.sync != "" && s.sync == message.Sync {
				a.game.choose(node, round)
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
			switch {
			case current && a.game.canPractice(node, message.PID, message.To):
				// A practice SEND touches only the practice queue (server.md 3.6).
				refill := a.game.practiceSend(node)
				if a.send(id, Message{Type: "result", Data: ResultData{message.Sync, message.RID, true}}) {
					a.send(id, Message{Type: "push", Data: refill})
				}
			case current && a.game.canTransfer(node, message.PID, message.To):
				// Commit before sending; publish RESULT before the transfer's notifications.
				changes := a.game.transfer(node, message.To, true)
				a.send(id, Message{Type: "result", Data: ResultData{message.Sync, message.RID, true}})
				for _, c := range changes {
					a.notify(c)
				}
				a.game.abortOverflowed()
			default:
				// A rejected SEND of the current generation resynchronizes the queue.
				if a.send(id, Message{Type: "result", Data: ResultData{message.Sync, message.RID, false}}) && current && node != 0 {
					a.resetFIFO(id, node)
				}
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
	if a.fatalErr != nil {
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
