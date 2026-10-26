package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ledUID(node NodeID) string { return fmt.Sprintf("%012X", uint64(0xA00000000000)+uint64(node)) }

func (h *harness) device(uid string) DeviceSnapshot {
	h.t.Helper()
	for _, d := range h.state().Devices {
		if d.ID == uid {
			return d
		}
	}
	h.t.Fatalf("missing device %s", uid)
	return DeviceSnapshot{}
}

func (h *harness) connectLED(id SessionID, uid string, node NodeID) {
	h.t.Helper()
	h.must(h.a.Connected(h.ctx, id))
	h.message(id, IncomingMessage{Type: "hello", Kind: LEDDevice, UID: uid})
	h.execute(Command{Operation: SetDeviceAssignment, Kind: LEDDevice, DeviceID: uid, Node: node})
}

func (h *harness) lines(id SessionID) []string {
	h.t.Helper()
	var lines []string
	for _, sent := range h.messages() {
		if id != "" && sent.session != id {
			continue
		}
		line, err := sent.message.MarshalLine()
		h.must(err)
		lines = append(lines, string(line))
	}
	return lines
}

func (h *harness) statusMessages(id SessionID) []StatusData {
	var statuses []StatusData
	for _, sent := range h.messages() {
		if sent.session == id && sent.message.Type == "status" {
			statuses = append(statuses, sent.message.Data.(StatusData))
		}
	}
	return statuses
}

func TestReadyAndStartConditions(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	if !h.state().CanStart {
		t.Fatal("all-auto game cannot start")
	}
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	if s := h.state(); s.CanStart || len(s.StartBlockers) != 1 || s.StartBlockers[0].Code != CodePlayersNotReady || !reflect.DeepEqual(s.StartBlockers[0].Nodes, []NodeID{1}) {
		t.Fatalf("missing unready reason: %+v", s)
	}
	if err := h.a.Execute(h.ctx, Command{Operation: Start}); err == nil {
		t.Fatal("unready manual player could start")
	}
	h.clear()
	h.message("p1", IncomingMessage{Type: "ready", Sync: strings.Repeat("F", 32)})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"status"}) || h.state().Nodes[0].Ready {
		t.Fatal("stale READY was accepted or resynchronized")
	}
	h.clear()
	h.ready("p1")
	if got := h.statusMessages("p1"); len(got) != 1 || !got[0].Ready || !h.state().CanStart {
		t.Fatalf("valid READY not committed: %v", got)
	}
	h.clear()
	h.ready("p1")
	if len(h.statusMessages("p1")) != 1 {
		t.Fatal("duplicate READY needs STATUS acknowledgement")
	}
	h.message("p1", IncomingMessage{Type: "resync"})
	h.message("p1", IncomingMessage{Type: "hello", UID: testUID})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: testUID, Node: 1})
	if !h.state().Nodes[0].Ready {
		t.Fatal("resync or unchanged settings cleared ready")
	}
	oldSync := h.reset("p1").Sync
	h.execute(Command{Operation: Start})
	if !h.state().Nodes[0].Ready {
		t.Fatal("Start cleared readiness")
	}
	if err := h.a.Execute(h.ctx, Command{Operation: SetNodeEnabled, Node: 1, Enabled: false}); err == nil {
		t.Fatal("Playing mode change accepted")
	}
	h.execute(Command{Operation: Reset})
	h.clear()
	h.message("p1", IncomingMessage{Type: "ready", Sync: oldSync})
	if h.state().Nodes[0].Ready || h.state().CanStart {
		t.Fatal("old READY survived game Reset")
	}
	h.ready("p1")
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: false})
	if s := h.state(); s.Nodes[0].Ready || !s.CanStart {
		t.Fatal("auto mode retained ready or blocked Start")
	}
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	if h.state().Nodes[0].Ready {
		t.Fatal("mode change restored ready")
	}
}

func TestStatusUsesLatestCommittedValues(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.start()
	h.clear()
	h.tick(3200 * time.Millisecond)
	statuses := h.statusMessages("p1")
	if len(statuses) != 1 || statuses[0].Remaining != 117 {
		t.Fatalf("catchup sent intermediate statuses: %v", statuses)
	}
	h.clear()
	h.tick(3300 * time.Millisecond)
	if len(h.statusMessages("p1")) != 0 {
		t.Fatal("unchanged display was sent on every Tick")
	}
	h.execute(Command{Operation: Finish})
	if statuses = h.statusMessages("p1"); len(statuses) != 1 || statuses[0].Remaining != 117 {
		t.Fatalf("Finish did not force frozen status: %v", statuses)
	}
	h.clear()
	h.tick(90 * time.Second)
	if len(h.statusMessages("p1")) != 0 || h.state().Remaining != 116700*time.Millisecond {
		t.Fatal("Finished time advanced")
	}
	h.execute(Command{Operation: Reset})
	if statuses = h.statusMessages("p1"); len(statuses) != 1 || statuses[0] != (StatusData{Remaining: 120}) {
		t.Fatalf("wrong initial status: %v", statuses)
	}
}

func TestAutomaticAssignmentReservationsAndRetry(t *testing.T) {
	h := newHarness(t)
	for i := 1; i <= 9; i++ {
		id := SessionID(fmt.Sprintf("p%d", i))
		uid := fmt.Sprintf("%012X", i)
		h.must(h.a.Connected(h.ctx, id))
		h.message(id, IncomingMessage{Type: "hello", UID: uid})
		want := NodeID(i)
		if i == 9 {
			want = 0
		}
		if h.device(uid).Node != want {
			t.Fatalf("provisional role %d: %+v", i, h.device(uid))
		}
	}
	h.must(h.a.Disconnected(h.ctx, "p1"))
	if h.device("000000000009").Node != 0 {
		t.Fatal("disconnected role was reused")
	}
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: "000000000001"})
	if h.device("000000000009").Node != 1 {
		t.Fatal("waiting device did not take newly freed role")
	}
	h.must(h.a.Connected(h.ctx, "p1-new"))
	h.message("p1-new", IncomingMessage{Type: "hello", UID: "000000000001"})
	if h.device("000000000001").Node != 0 {
		t.Fatal("explicitly unassigned UID regained a role")
	}
	h.execute(Command{Operation: Reset})
	if h.device("000000000001").Node != 0 {
		t.Fatal("Reset lost automatic-assignment exclusion")
	}
	h.connectLED("led", "A00000000001", 1)
	if h.device("A00000000001").Node != 1 {
		t.Fatal("player and LED namespaces conflict")
	}
	if err := h.a.Execute(h.ctx, Command{Operation: SetDeviceAssignment, Kind: LEDDevice, DeviceID: "000000000009", Node: 3}); err == nil {
		t.Fatal("cross-kind assignment accepted")
	}
}

func TestFirstConnectionDuringPlayingWaitsForFinish(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.must(h.a.Connected(h.ctx, "p1"))
	h.message("p1", IncomingMessage{Type: "hello", UID: testUID})
	if h.device(testUID).Node != 0 {
		t.Fatal("first connection was assigned during Playing")
	}
	h.clear()
	h.execute(Command{Operation: Finish})
	if h.device(testUID).Node != 1 {
		t.Fatal("pending assignment not retried at Finish")
	}
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"config", "reset", "phase", "status", "summary"}) {
		t.Fatal(got)
	}
}

func TestSwapAndReplacementKeepNodeResults(t *testing.T) {
	h := newHarness(t)
	uid2 := "112233AABBCC"
	h.connect("p1", testUID, 1)
	h.connect("p2", uid2, 2)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
	h.start()
	h.tick(time.Second)
	h.must(h.a.submit(h.ctx, false, func(time.Time) error { h.a.game.nodes[1].score = 15; h.a.game.nodes[2].score = 8; return nil }))
	h.execute(Command{Operation: Finish})
	before := h.state()
	h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: uid2})
	if h.device(testUID).Node != 2 || h.device(uid2).Node != 1 {
		t.Fatal("swap did not exchange both roles")
	}
	after := h.state()
	for i := 0; i < 2; i++ {
		if after.Nodes[i].Score != before.Nodes[i].Score || after.Nodes[i].FIFOUsage != before.Nodes[i].FIFOUsage || after.Nodes[i].RankingEligible != before.Nodes[i].RankingEligible || after.Nodes[i].Enabled || after.Nodes[i].Ready {
			t.Fatal("swap moved or cleared game results")
		}
	}
	h.must(h.a.Disconnected(h.ctx, "p1")) // A broken original board can be replaced.
	h.connect("spare", "223344AABBCC", 3)
	h.execute(Command{Operation: ReplaceDeviceAssignment, DeviceID: testUID, OtherDeviceID: "223344AABBCC"})
	if h.device(testUID).Node != 0 || h.device("223344AABBCC").Node != 2 {
		t.Fatal("replacement mapping wrong")
	}
	h.must(h.a.Connected(h.ctx, "old"))
	h.message("old", IncomingMessage{Type: "hello", UID: testUID})
	if h.device(testUID).Node != 0 {
		t.Fatal("old board reclaimed a role after replacement")
	}
	snapshot := h.state()
	if err := h.a.Execute(h.ctx, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: uid2}); err == nil {
		t.Fatal("swap with unassigned device accepted")
	}
	if !reflect.DeepEqual(snapshot, h.state()) {
		t.Fatal("rejected exchange changed state")
	}
}

func TestPlayerPreviewRestoresBeforeStart(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233AABBCC", 2)
	h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
	before := h.state()
	h.clear()
	h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"config", "reset", "phase", "status"}) {
		t.Fatal(got)
	}
	previewReset := h.reset("p1")
	if len(previewReset.Packets) != 0 || !h.messages()[0].message.Data.(ConfigData).Preview {
		t.Fatalf("preview must use real FIFO and explicit configuration: %+v", previewReset)
	}
	if got := h.state(); got.Nodes[0] != before.Nodes[0] || got.Preview == nil || got.Preview.Node != 1 {
		t.Fatal("preview modified game state")
	}
	h.clear()
	if err := h.a.Execute(h.ctx, Command{Operation: Start}); err == nil {
		t.Fatal("unready Start accepted")
	}
	if h.state().Preview == nil || len(h.messages()) != 0 {
		t.Fatal("rejected Start stopped preview")
	}
	h.ready("p2")
	h.clear()
	h.execute(Command{Operation: Start})
	var types []string
	for _, m := range h.messages() {
		if m.session == "p1" {
			types = append(types, m.message.Type)
		}
	}
	if !reflect.DeepEqual(types, []string{"config", "reset", "phase", "status", "phase", "status"}) {
		t.Fatalf("restore must precede Playing: %v", types)
	}
	var phases []Phase
	for _, m := range h.messages() {
		if m.session == "p1" && m.message.Type == "phase" {
			phases = append(phases, m.message.Data.(PhaseData).Phase)
		}
	}
	if !reflect.DeepEqual(phases, []Phase{Preparing, Playing}) {
		t.Fatalf("wrong recovery phases: %v", phases)
	}
	if h.state().Preview != nil || len(h.reset("p1").Packets) != 0 || h.reset("p1").Sync == previewReset.Sync {
		t.Fatal("preview not restored with a new generation")
	}
	h.tick(time.Second)
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		if h.a.game.nextPID != 8 {
			return fmt.Errorf("preview consumed a PID: %d", h.a.game.nextPID)
		}
		return nil
	}))
}

func TestUnassignedPreviewCannotOperateAnotherPlayersNode(t *testing.T) {
	h := newHarness(t)
	h.connect("owner", testUID, 1)
	h.connect("spare", "112233AABBCC", 2)
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: "112233AABBCC"})
	h.message("spare", IncomingMessage{Type: "hello", UID: "112233AABBCC"})
	h.clear()
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "112233AABBCC"})
	if h.device("112233AABBCC").Node != 0 || h.device(testUID).Node != 1 {
		t.Fatal("temporary P1 changed the assignment table")
	}
	h.clear()
	h.ready("spare")
	if len(h.messages()) != 0 {
		t.Fatal("unassigned READY received game notification")
	}
	h.mu.Lock()
	generation := h.generation["spare"]
	h.mu.Unlock()
	h.message("spare", IncomingMessage{Type: "send", Sync: generation, RID: 0, PID: 0, To: 2})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"result"}) || h.messages()[0].message.Data.(ResultData).OK {
		t.Fatal("preview was allowed to operate P1")
	}
	h.clear()
	h.execute(Command{Operation: SetDevicePreview})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"identify"}) {
		t.Fatal(got)
	}
	h.message("spare", IncomingMessage{Type: "hello", UID: "112233AABBCC"})
	if h.device("112233AABBCC").Node != 0 || h.state().Preview != nil {
		t.Fatal("preview termination assigned or restarted the device")
	}
}

func TestLEDPreviewTimingRecoveryAndReset(t *testing.T) {
	h := newHarness(t)
	// Move the first LED to B1; its former P1 becomes free.
	h.connectLED("led", "A00000000001", 9)
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		h.a.game.nodes[9].fifo = []*packet{{PacketView: PacketView{0, 2}}, {PacketView: PacketView{1, 3}}}
		return nil
	}))
	h.clear()
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "A00000000001"})
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{"CONFIG B1 true"}) {
		t.Fatal(got)
	}
	h.clear()
	h.tick(500 * time.Millisecond)
	if got := h.lines("led"); len(got) != 0 {
		t.Fatal(got)
	}
	h.clear()
	h.tick(3600 * time.Millisecond)
	if got := h.lines("led"); len(got) != 0 {
		t.Fatalf("replayed missed cycles: %v", got)
	}
	h.clear()
	h.tick(4 * time.Second)
	if got := h.lines("led"); len(got) != 0 {
		t.Fatal(got)
	}
	h.clear()
	h.execute(Command{Operation: SetDevicePreview})
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{"CONFIG B1 false", "BUFFER 2"}) {
		t.Fatal(got)
	}
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "A00000000001"})
	h.clear()
	h.execute(Command{Operation: Reset})
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{"CONFIG B1 false", "BUFFER 0"}) {
		t.Fatalf("Reset duplicated restore or resent CONFIG: %v", got)
	}
	h.clear()
	h.tick(5 * time.Second)
	if len(h.messages()) != 0 || h.state().Preview != nil {
		t.Fatal("preview continued after Reset")
	}
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "A00000000001"})
	h.clear()
	h.execute(Command{Operation: SetDeviceAssignment, Kind: LEDDevice, DeviceID: "A00000000001", Node: 10})
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{"CONFIG B2 false", "BUFFER 0"}) {
		t.Fatalf("reassignment restored the old role: %v", got)
	}
}

func TestPreviewEndOnResyncHelloAndDisconnect(t *testing.T) {
	for _, event := range []string{"resync", "hello", "disconnect", "manual"} {
		t.Run(event, func(t *testing.T) {
			h := newHarness(t)
			h.connect("p1", testUID, 1)
			h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
			h.clear()
			switch event {
			case "resync":
				h.message("p1", IncomingMessage{Type: "resync"})
			case "hello":
				h.message("p1", IncomingMessage{Type: "hello", UID: testUID})
			case "disconnect":
				h.must(h.a.Disconnected(h.ctx, "p1"))
			case "manual":
				h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
			}
			if h.state().Preview != nil {
				t.Fatal("preview remained active")
			}
			want := []string{"config", "reset", "phase", "status"}
			if event == "disconnect" {
				want = nil
			}
			if got := messageTypes(h.messages()); !reflect.DeepEqual(got, want) {
				t.Fatalf("duplicate recovery: %v", got)
			}
		})
	}
}

func TestLEDLinkMappingAndProtocol(t *testing.T) {
	h := newHarness(t)
	for _, node := range ledNodes {
		h.connectLED(SessionID(node.wireName()), ledUID(node), node)
	}
	h.clear()
	count := 0
	for from := NodeID(1); from <= PlayerCount; from++ {
		for _, to := range neighbors[from] {
			if to.player() && to < from {
				continue
			}
			count++
			h.must(h.a.submit(h.ctx, false, func(time.Time) error { h.a.light(from, to, 8); h.a.light(to, from, 8); return nil }))
			messages := h.messages()
			pair := messages[len(messages)-2:]
			if pair[0].session != pair[1].session {
				t.Fatal("opposite directions picked different controllers")
			}
			a, b := pair[0].message.Data.(LightData), pair[1].message.Data.(LightData)
			if a.Neighbor != b.Neighbor || a.Destination != 8 || a.Direction == b.Direction {
				t.Fatal("invalid link direction or color")
			}
		}
	}
	if count != 24 {
		t.Fatalf("covered %d links", count)
	}
	line, err := (Message{Type: "identify", Data: struct{}{}}).MarshalLine()
	h.must(err)
	if string(line) != `{"type":"identify","data":{}}` {
		t.Fatal(string(line))
	}
	for _, message := range []Message{{Type: "CONFIG", Data: LEDConfigData{Node: 2}}, {Type: "LIGHT", Data: LightData{1, 0, "incoming"}}, {Type: "BUFFER", Data: BufferData{5}}} {
		if _, err := message.MarshalLine(); err == nil {
			t.Fatalf("invalid LED output accepted: %+v", message)
		}
	}
}

func TestLEDCommandsFollowGameTransfers(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.connectLED("led", "A00000000001", 9)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.start()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		for node := NodeID(2); node <= PlayerCount; node++ {
			h.a.game.nodes[node].nextGeneration = 0
		}
		return nil
	}))
	h.tick(time.Second)
	h.mu.Lock()
	generation := h.generation["p1"]
	h.mu.Unlock()
	h.clear()
	h.message("p1", IncomingMessage{Type: "send", Sync: generation, RID: 0, PID: 0, To: 9})
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{"BUFFER 1", "LIGHT P1 P2 incoming"}) {
		t.Fatal(got)
	}
	if h.messages()[0].message.Type != "result" {
		t.Fatal("transfer notification preceded RESULT")
	}
	h.clear()
	h.tick(2 * time.Second)
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{"LIGHT P2 P2 outgoing", "BUFFER 0"}) {
		t.Fatal(got)
	}
	if h.state().Nodes[0].Score != 15 {
		t.Fatal("Bot did not deliver the packet")
	}
}

func TestNotificationFailuresDoNotUndoAssignmentsOrStart(t *testing.T) {
	t.Run("atomic swap", func(t *testing.T) {
		h := newHarness(t)
		h.connect("p1", testUID, 1)
		h.connect("p2", "112233AABBCC", 2)
		h.mu.Lock()
		h.failType = "config"
		h.mu.Unlock()
		h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: "112233AABBCC"})
		if a, b := h.device(testUID), h.device("112233AABBCC"); a.Node != 2 || b.Node != 1 || a.Connected || b.Connected {
			t.Fatal("failed notifications undid part of a committed swap")
		}
	})
	for _, failedMessage := range []string{"reset", "phase", "status"} {
		t.Run("preview restore/"+failedMessage, func(t *testing.T) {
			h := newHarness(t)
			h.connect("p1", testUID, 1)
			h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
			h.mu.Lock()
			h.failType = failedMessage
			h.mu.Unlock()
			h.execute(Command{Operation: Start})
			if h.state().Phase != Playing || h.state().Preview != nil || h.device(testUID).Connected {
				t.Fatal("restore failure left preview active or rolled back Start")
			}
		})
	}
}

func TestInvalidPreviewSwitchKeepsCurrentTarget(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233AABBCC", 2)
	h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
	h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
	h.clear()
	if err := h.a.Execute(h.ctx, Command{Operation: SetDevicePreview, DeviceID: "112233AABBCC"}); err == nil {
		t.Fatal("preview of manual player accepted")
	}
	h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
	if len(h.messages()) != 0 || h.state().Preview.DeviceID != testUID {
		t.Fatal("invalid or repeated preview request restarted the display")
	}
	h.execute(Command{Operation: SetDevicePreview})
	h.clear()
	h.execute(Command{Operation: SetDevicePreview})
	if len(h.messages()) != 0 {
		t.Fatal("stopping inactive preview sent recovery messages")
	}
}

func TestLEDPreviewUsesExplicitConfig(t *testing.T) {
	for _, node := range ledNodes {
		t.Run(node.wireName(), func(t *testing.T) {
			h := newHarness(t)
			h.connectLED("led", "A00000000001", node)
			h.clear()
			h.execute(Command{Operation: SetDevicePreview, DeviceID: "A00000000001"})
			if got := h.lines("led"); !reflect.DeepEqual(got, []string{"CONFIG " + node.wireName() + " true"}) {
				t.Fatalf("unexpected preview command: %v", got)
			}
		})
	}
}

func TestSendFailureDoesNotRewriteCatchUpHistory(t *testing.T) {
	for _, input := range []string{"tick", "command", "message", "game end"} {
		t.Run(input, func(t *testing.T) {
			actual, reference := newHarness(t), newHarness(t)
			for _, h := range []*harness{actual, reference} {
				h.connect("p1", testUID, 1)
				h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
				h.start()
			}
			actual.mu.Lock()
			actual.failType = "push"
			actual.mu.Unlock()
			elapsed := 10 * time.Second
			if input == "game end" {
				elapsed = GameDuration + time.Second
			}
			for _, h := range []*harness{actual, reference} {
				h.clock.Store(int64(elapsed))
				switch input {
				case "command":
					h.execute(Command{Operation: SetBotRate, Node: 9, Rate: 1})
				case "message":
					h.message("p1", IncomingMessage{Type: "resync"})
				default:
					h.tick(elapsed)
				}
			}
			// The connection fails at the processing time, after the same manual history.
			reference.must(reference.a.Disconnected(reference.ctx, "p1"))
			got, want := actual.state(), reference.state()
			if !reflect.DeepEqual(got.Nodes, want.Nodes) {
				t.Fatalf("send failure rewrote history: actual=%+v reference=%+v", got.Nodes[0], want.Nodes[0])
			}
			expectedFIFO := 10
			if input == "game end" {
				expectedFIFO = 119
			}
			if got.Nodes[0].Score != 0 || got.Nodes[0].FIFOUsage != expectedFIFO {
				t.Fatalf("manual history was auto-forwarded: %+v", got.Nodes[0])
			}
			if input == "game end" {
				if got.Phase != Finished || !got.Nodes[0].RankingEligible {
					t.Fatal("post-finish failure changed final eligibility")
				}
			} else {
				// Automatic service starts one full service interval after detection.
				actual.tick(10499 * time.Millisecond)
				reference.tick(10499 * time.Millisecond)
				if actual.state().Nodes[0].Score != 0 {
					t.Fatal("automatic forwarding started too early")
				}
				actual.tick(10500 * time.Millisecond)
				reference.tick(10500 * time.Millisecond)
				if !reflect.DeepEqual(actual.state().Nodes, reference.state().Nodes) || actual.state().Nodes[0].Score != 15 {
					t.Fatal("wrong first automatic deadline")
				}
			}
			actual.mu.Lock()
			closed := append([]SessionID(nil), actual.closed...)
			actual.mu.Unlock()
			if !reflect.DeepEqual(closed, []SessionID{"p1"}) {
				t.Fatalf("physical session closed repeatedly: %v", closed)
			}
		})
	}
}

func TestPreviewSwitchCompletesOldRecoveryFirst(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233AABBCC", 2)
	h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
	h.clear()
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "112233AABBCC"})
	messages := h.messages()
	if len(messages) != 8 || !reflect.DeepEqual(messageTypes(messages), []string{"config", "reset", "phase", "status", "config", "reset", "phase", "status"}) {
		t.Fatalf("incomplete recovery before preview switch: %v", messageTypes(messages))
	}
	for i, m := range messages {
		want := SessionID("p1")
		if i >= 4 {
			want = "p2"
		}
		if m.session != want {
			t.Fatalf("notification %d went to %s, want %s", i, m.session, want)
		}
	}
}
