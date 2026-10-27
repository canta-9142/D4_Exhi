package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testUID = "001122AABBCC"

type sentMessage struct {
	session SessionID
	message Message
}

type harness struct {
	t          *testing.T
	a          *App
	ctx        context.Context
	cancel     context.CancelFunc
	finished   chan error
	ticks      chan time.Time
	clock      atomic.Int64
	mu         sync.Mutex
	sent       []sentMessage
	closed     []SessionID
	failType   string
	generation map[SessionID]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	h := &harness{t: t, ctx: ctx, cancel: cancel, finished: make(chan error, 1), ticks: make(chan time.Time), generation: make(map[SessionID]string)}
	h.a = New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(id SessionID, message Message) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.failType == message.Type {
				return errors.New("write queue full")
			}
			if _, err := message.MarshalLine(); err != nil {
				return err
			}
			if message.Type == "reset" {
				h.generation[id] = message.Data.(ResetData).Sync
			}
			h.sent = append(h.sent, sentMessage{id, message})
			return nil
		},
		Close: func(id SessionID) { h.mu.Lock(); defer h.mu.Unlock(); h.closed = append(h.closed, id) },
	})
	h.a.now = func() time.Time { return epoch.Add(time.Duration(h.clock.Load())) }
	h.a.ticks = h.ticks
	h.a.game.random = func(int) int { return 0 }
	go func() { h.finished <- h.a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-h.finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("app did not stop")
		}
	})
	return h
}

func (h *harness) must(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}
func (h *harness) execute(c Command) {
	h.t.Helper()
	if c.Operation == SetDeviceAssignment && c.Kind == "" {
		c.Kind = PlayerDevice
	}
	h.must(h.a.Execute(h.ctx, c))
}
func (h *harness) message(id SessionID, m IncomingMessage) {
	h.t.Helper()
	if m.Type == "hello" && m.Kind == "" {
		m.Kind = PlayerDevice
	}
	h.must(h.a.MessageReceived(h.ctx, id, m))
}
func (h *harness) state() Snapshot {
	h.t.Helper()
	s, err := h.a.State(h.ctx)
	h.must(err)
	return s
}
func (h *harness) messages() []sentMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]sentMessage(nil), h.sent...)
}
func (h *harness) clear() { h.mu.Lock(); defer h.mu.Unlock(); h.sent = nil }
func (h *harness) tick(at time.Duration) {
	h.t.Helper()
	h.clock.Store(int64(at))
	select {
	case h.ticks <- epoch: // Tick timestamps must not control game time.
	case <-h.ctx.Done():
		h.t.Fatal(h.ctx.Err())
	}
	h.state() // Wait until the tick's state changes have been committed.
}
func (h *harness) connect(id SessionID, uid string, node NodeID) {
	h.t.Helper()
	h.must(h.a.Connected(h.ctx, id))
	h.message(id, IncomingMessage{Type: "hello", UID: uid})
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: uid, Node: node})
}

func (h *harness) ready(id SessionID) {
	h.t.Helper()
	h.mu.Lock()
	generation := h.generation[id]
	h.mu.Unlock()
	h.message(id, IncomingMessage{Type: "ready", Sync: generation})
}
func (h *harness) start() {
	h.t.Helper()
	s := h.state()
	for _, d := range s.Devices {
		if d.Kind == PlayerDevice && d.Node != 0 && s.Nodes[d.Node-1].Enabled {
			h.ready(d.Session)
		}
	}
	h.execute(Command{Operation: Start})
}
func (h *harness) reset(id SessionID) ResetData {
	h.t.Helper()
	messages := h.messages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].session == id && messages[i].message.Type == "reset" {
			return messages[i].message.Data.(ResetData)
		}
	}
	h.t.Fatal("missing RESET")
	return ResetData{}
}
func messageTypes(messages []sentMessage) []string {
	var result []string
	for _, m := range messages {
		result = append(result, m.message.Type)
	}
	return result
}

func TestIdentificationAssignmentAndSnapshot(t *testing.T) {
	h := newHarness(t)
	h.must(h.a.Connected(h.ctx, "s1"))
	h.message("s1", IncomingMessage{Type: "send", RID: 100}) // Old input before HELLO.
	h.tick(time.Second)
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"identify", "identify"}) {
		t.Fatal(got)
	}
	h.message("s1", IncomingMessage{Type: "hello", UID: testUID})
	s := h.state()
	if len(s.Devices) != 1 || !s.Devices[0].Connected || s.Devices[0].Node != 1 {
		t.Fatalf("missing provisional assignment: %+v", s.Devices)
	}
	h.clear()
	h.message("s1", IncomingMessage{Type: "resync"})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"reset", "phase", "status"}) {
		t.Fatal(got)
	}
	reset := h.reset("s1")
	data, err := json.Marshal(Message{Type: "reset", Data: reset})
	h.must(err)
	if !strings.Contains(string(data), `"packets":[]`) || !validHex(reset.Sync, 32) {
		t.Fatalf("bad RESET: %s", data)
	}
	if h.state().Nodes[0].Enabled {
		t.Fatal("assignment enabled manual control")
	}
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.clear()
	h.tick(2 * time.Second)
	if len(h.messages()) != 0 {
		t.Fatal("identify continued after HELLO")
	}
	s.Nodes[0].Score = 1234
	s.Devices[0].ID = "mutated"
	if current := h.state(); current.Nodes[0].Score != 0 || current.Devices[0].ID != testUID {
		t.Fatal("Snapshot aliases app state")
	}
}

func TestStateDoesNotAdvanceButInputDoes(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.clock.Store(int64(3 * time.Second))
	if s := h.state(); s.Elapsed != 0 || s.Nodes[0].FIFOUsage != 0 {
		t.Fatal("State advanced game time")
	}
	// Even a rejected command must first process elapsed time.
	if err := h.a.Execute(h.ctx, Command{Operation: Start}); err == nil {
		t.Fatal("duplicate Start accepted")
	}
	if s := h.state(); s.Elapsed != 3*time.Second {
		t.Fatal("input did not advance game time")
	}
	h.clock.Store(int64(time.Second))
	h.execute(Command{Operation: SetBotRate, Node: 9, Rate: 2})
	if h.state().Elapsed != 3*time.Second {
		t.Fatal("time moved backwards")
	}
	h.clock.Store(int64(GameDuration))
	if err := h.a.Execute(h.ctx, Command{Operation: Finish}); err == nil {
		t.Fatal("Finish accepted after automatic finish")
	}
	s := h.state()
	if s.Phase != Finished || s.Remaining != 0 {
		t.Fatalf("game did not finish: %+v", s)
	}
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: false})
	if !reflect.DeepEqual(s.Nodes, h.state().Nodes) {
		t.Fatal("Finished results changed")
	}
}

func TestSendGenerationsDuplicatesAndReset(t *testing.T) {
	h := newHarness(t)
	h.connect("s1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	initialSync := h.reset("s1").Sync
	h.start()
	h.tick(time.Second)
	h.clear()
	send := IncomingMessage{Type: "send", Sync: initialSync, RID: 0, PID: 0, To: 2}
	h.message("s1", send)
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"result", "status"}) {
		t.Fatal(got)
	}
	if !h.messages()[0].message.Data.(ResultData).OK || h.state().Nodes[0].Score != 15 {
		t.Fatal("valid SEND did not deliver")
	}
	h.clear()
	h.message("s1", send)
	if len(h.messages()) != 0 || h.state().Nodes[0].Score != 15 {
		t.Fatal("duplicate SEND was applied/responded to")
	}

	// A current-generation rejection sends RESULT, then a new RESET.
	send.RID++
	h.message("s1", send)
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"result", "reset"}) {
		t.Fatal(got)
	}
	newSync := h.reset("s1").Sync
	if newSync == initialSync {
		t.Fatal("RESET reused sync generation")
	}
	h.clear()
	send.RID++
	h.message("s1", send)
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"result"}) || h.messages()[0].message.Data.(ResultData).OK {
		t.Fatal("stale generation did not get only a rejection")
	}

	// Repeated HELLO preserves request history while synchronizing anew.
	h.message("s1", IncomingMessage{Type: "hello", UID: testUID})
	send.Sync = h.reset("s1").Sync
	h.clear()
	h.message("s1", send)
	if len(h.messages()) != 0 {
		t.Fatal("HELLO cleared request history")
	}
	h.execute(Command{Operation: Reset})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"phase", "reset", "status"}) {
		t.Fatal(got)
	}
	resetSync := h.reset("s1").Sync
	if resetSync == newSync {
		t.Fatal("game Reset reused generation")
	}
	h.clear()
	h.message("s1", send)
	if len(h.messages()) != 0 {
		t.Fatal("Reset cleared request history")
	}
	h.start()
	h.tick(2 * time.Second)
	if h.state().Nodes[0].FIFOUsage != 1 {
		t.Fatal("new game did not generate FIFO")
	}
	send.RID++
	send.Sync = resetSync
	h.clear()
	h.message("s1", send) // Old game's PID with the current generation.
	if h.messages()[0].message.Data.(ResultData).OK || h.state().Nodes[0].FIFOUsage != 1 {
		t.Fatal("old PID applied in new game")
	}
}

func TestDisconnectDuplicateUIDAndReassignment(t *testing.T) {
	h := newHarness(t)
	h.connect("s1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.must(h.a.Connected(h.ctx, "duplicate"))
	h.clear()
	h.message("duplicate", IncomingMessage{Type: "hello", UID: testUID})
	if len(h.messages()) != 0 || h.state().Devices[0].Session != "s1" {
		t.Fatal("duplicate UID replaced live session")
	}
	h.must(h.a.Disconnected(h.ctx, "duplicate"))
	if !h.state().Nodes[0].Enabled {
		t.Fatal("duplicate disconnect affected owner")
	}
	h.start()
	h.must(h.a.Disconnected(h.ctx, "s1"))
	if s := h.state(); s.Nodes[0].Enabled || s.Nodes[0].RankingEligible {
		t.Fatal("disconnect did not switch to automatic control")
	}
	h.must(h.a.Connected(h.ctx, "s2"))
	h.message("s2", IncomingMessage{Type: "hello", UID: testUID})
	if h.state().Nodes[0].Enabled {
		t.Fatal("reconnect automatically restored manual control")
	}
	if err := h.a.Execute(h.ctx, Command{Operation: SetNodeEnabled, Node: 1, Enabled: true}); err == nil {
		t.Fatal("Playing mode change accepted")
	}
	if h.state().Nodes[0].RankingEligible {
		t.Fatal("reconnect restored ranking eligibility")
	}
	h.must(h.a.Disconnected(h.ctx, "s1"))
	h.message("s1", IncomingMessage{Type: "hello", UID: testUID})
	if h.state().Nodes[0].Enabled {
		t.Fatal("stale session modified reconnected node")
	}
	if err := h.a.Execute(h.ctx, Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 2}); err == nil {
		t.Fatal("reassignment accepted while Playing")
	}
	h.execute(Command{Operation: Finish})
	h.clear()
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 2})
	if s := h.state(); s.Nodes[0].Enabled || s.Nodes[1].Enabled || s.Devices[0].Node != 2 {
		t.Fatal("wrong reassignment state")
	}
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"config", "reset", "phase", "status", "summary"}) {
		t.Fatal(got)
	}
	h.clear()
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"identify"}) {
		t.Fatal(got)
	}
}

func TestSendFailureKeepsCommittedGameState(t *testing.T) {
	h := newHarness(t)
	h.connect("s1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	syncID := h.reset("s1").Sync
	h.start()
	h.tick(time.Second)
	h.mu.Lock()
	h.failType = "result"
	h.mu.Unlock()
	h.message("s1", IncomingMessage{Type: "send", Sync: syncID, RID: 1, PID: 0, To: 2})
	s := h.state()
	if s.Nodes[0].Score != 15 || s.Nodes[0].FIFOUsage != 0 || s.Nodes[0].Enabled || s.Devices[0].Connected {
		t.Fatalf("bad state after send failure: %+v", s)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !reflect.DeepEqual(h.closed, []SessionID{"s1"}) {
		t.Fatal("underlying connection was not closed")
	}
}

func TestBotRateReschedulesFromChangeTime(t *testing.T) {
	h := newHarness(t)
	h.start()
	// Inject one bot arrival so that other nodes cannot obscure its deadline.
	h.must(h.a.submit(h.ctx, true, func(time.Time) error {
		for id := NodeID(1); id <= PlayerCount; id++ {
			h.a.game.nodes[id].nextGeneration = 0
		}
		h.a.game.append(9, &packet{PacketView: PacketView{100, 2}, origin: 1, visited: 1<<1 | 1<<9})
		return nil
	}))
	h.clock.Store(int64(250 * time.Millisecond))
	h.execute(Command{Operation: SetBotRate, Node: 9, Rate: 2})
	h.clock.Store(int64(500 * time.Millisecond))
	h.execute(Command{Operation: SetBotRate, Node: 9, Rate: 2}) // An unchanged setting must not postpone service.
	h.tick(749 * time.Millisecond)
	if h.state().Nodes[8].FIFOUsage != 1 {
		t.Fatal("rate change transferred too early")
	}
	h.tick(750 * time.Millisecond)
	if h.state().Nodes[8].FIFOUsage != 0 || h.state().Nodes[0].Score != 15 {
		t.Fatal("rate change did not transfer at new deadline")
	}
}

func TestTransferNotifications(t *testing.T) {
	for _, mode := range []string{"automatic", "forward to player", "loss at bot"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			h.connect("s1", testUID, 1)
			if mode != "automatic" {
				h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
			}
			if mode == "forward to player" {
				h.connect("s2", "112233AABBCC", 2)
				h.a.game.random = func(int) int { return 1 } // P1 generates a packet for P3.
			}
			syncID := h.reset("s1").Sync
			h.start()
			h.tick(time.Second)
			if mode == "loss at bot" {
				h.must(h.a.submit(h.ctx, false, func(time.Time) error {
					h.a.game.nodes[9].fifo = make([]*packet, botCapacity)
					return nil
				}))
			}
			h.clear()
			if mode == "automatic" {
				h.tick(1500 * time.Millisecond)
				if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"reset", "status"}) {
					t.Fatal(got)
				}
				if len(h.reset("s1").Packets) != 0 {
					t.Fatal("automatic removal left a cached packet")
				}
			} else {
				to := NodeID(2)
				if mode == "loss at bot" {
					to = 9
				}
				h.message("s1", IncomingMessage{Type: "send", Sync: syncID, RID: 0, PID: 0, To: to})
				messages := h.messages()
				if !messages[0].message.Data.(ResultData).OK {
					t.Fatal("valid SEND rejected")
				}
				if mode == "forward to player" {
					if got := messageTypes(messages); !reflect.DeepEqual(got, []string{"result", "push"}) || messages[1].session != "s2" {
						t.Fatalf("wrong transfer notification order: %v", messages)
					}
				} else if len(messages) != 1 || h.state().Nodes[0].Score != 0 {
					t.Fatal("lost packet was pushed or scored")
				}
			}
			if h.state().Nodes[0].FIFOUsage != 0 {
				t.Fatal("valid transfer did not consume FIFO head")
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	h := newHarness(t)
	h.connect("s1", testUID, 1)
	for _, c := range []Command{
		{Operation: Finish}, {Operation: "unknown"},
		{Operation: SetNodeEnabled, Node: 9}, {Operation: SetNodeEnabled, Node: 0},
		{Operation: SetNodeEnabled, Node: 2, Enabled: true},
		{Operation: SetBotRate, Node: 1, Rate: 1},
		{Operation: SetBotRate, Node: 9, Rate: 0}, {Operation: SetBotRate, Node: 9, Rate: -1},
		{Operation: SetBotRate, Node: 9, Rate: math.NaN()}, {Operation: SetBotRate, Node: 9, Rate: math.Inf(1)},
		{Operation: SetDeviceAssignment, DeviceID: "invalid", Node: 1},
		{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 9},
		{Operation: SetDeviceAssignment, DeviceID: "112233AABBCC", Node: 1},
	} {
		if err := h.a.Execute(h.ctx, c); err == nil {
			t.Fatalf("invalid command accepted: %+v", c)
		}
	}
	if h.state().Phase != Preparing {
		t.Fatal("rejected command changed phase")
	}
}

func TestChangedUIDClosesSession(t *testing.T) {
	h := newHarness(t)
	h.connect("s1", testUID, 1)
	if err := h.a.MessageReceived(h.ctx, "s1", IncomingMessage{Type: "hello", Kind: PlayerDevice, UID: "112233AABBCC"}); err == nil {
		t.Fatal("changed UID accepted")
	}
	if h.state().Devices[0].Connected {
		t.Fatal("changed UID left session connected")
	}
}

func TestConcurrentAccessAndCancellation(t *testing.T) {
	h := newHarness(t)
	var callers sync.WaitGroup
	for i := 0; i < 12; i++ {
		callers.Go(func() {
			for j := 0; j < 20; j++ {
				_, err := h.a.State(h.ctx)
				if err != nil {
					t.Error(err)
					return
				}
				if err := h.a.Execute(h.ctx, Command{Operation: SetBotRate, Node: 9, Rate: 2}); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	callers.Wait()
	canceled, cancel := context.WithCancel(h.ctx)
	cancel()
	if err := h.a.Execute(canceled, Command{Operation: Start}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	if h.state().Phase != Preparing {
		t.Fatal("pre-canceled request executed")
	}
	h.cancel()
	select {
	case <-h.a.done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	if _, err := h.a.State(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped State: %v", err)
	}
}
