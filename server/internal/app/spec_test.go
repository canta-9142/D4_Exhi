package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func (h *harness) messagesFor(id SessionID) []Message {
	var messages []Message
	for _, m := range h.messages() {
		if m.session == id {
			messages = append(messages, m.message)
		}
	}
	return messages
}

func (h *harness) summary(id SessionID) SummaryData {
	h.t.Helper()
	messages := h.messagesFor(id)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == "summary" {
			return messages[i].Data.(SummaryData)
		}
	}
	h.t.Fatal("missing SUMMARY")
	return SummaryData{}
}

func expectCode(t *testing.T, err error, code Code) *Rejection {
	t.Helper()
	var refusal *Rejection
	if !errors.As(err, &refusal) || refusal.Code != code {
		t.Fatalf("error=%v, want code=%s", err, code)
	}
	return refusal
}

func TestFinalRanksExcludeIneligibleAndUseCompetitionRanking(t *testing.T) {
	for _, tc := range []struct {
		name     string
		eligible []NodeID
		want     []int
	}{
		{"ties with high ineligible score", []NodeID{1, 2, 3}, []int{1, 1, 3, 0, 0, 0, 0, 0}},
		{"no eligible players", nil, []int{0, 0, 0, 0, 0, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGame(func(change) {})
			for _, id := range tc.eligible {
				g.setManual(id, true)
				g.nodes[id].ready = true
			}
			if err := g.start(epoch); err != nil {
				t.Fatal(err)
			}
			for i, score := range []int{100, 100, 80, 1000, 0, 0, 0, 0} {
				g.nodes[i+1].score = score
			}
			if err := g.finish(FinishManual); err != nil {
				t.Fatal(err)
			}
			for id := NodeID(1); id <= PlayerCount; id++ {
				if g.nodes[id].rank != tc.want[id-1] {
					t.Fatalf("%s rank=%d, want %d", id.wireName(), g.nodes[id].rank, tc.want[id-1])
				}
				before := g.nodes[id]
				g.setManual(id, !before.manual)
				if g.nodes[id].rank != before.rank || g.nodes[id].eligible != before.eligible || g.nodes[id].score != before.score {
					t.Fatal("Finished result changed with mode")
				}
			}
			g.reset()
			for id := NodeID(1); id <= PlayerCount; id++ {
				if g.nodes[id].rank != 0 {
					t.Fatal("Reset retained rank")
				}
			}
		})
	}
}

func TestSummarySynchronizationAndFrozenResults(t *testing.T) {
	h := newHarness(t)
	for id := NodeID(1); id <= 3; id++ {
		h.connect(SessionID(id.wireName()), fmt.Sprintf("%012X", id), id)
		h.execute(Command{Operation: SetNodeEnabled, Node: id, Enabled: true})
	}
	h.start()
	h.must(h.a.submit(h.ctx, false, func(time.Time) error {
		h.a.game.nodes[1].score, h.a.game.nodes[2].score, h.a.game.nodes[3].score, h.a.game.nodes[4].score = 100, 100, 80, 1000
		return nil
	}))
	h.clear()
	h.execute(Command{Operation: Finish})
	for id := NodeID(1); id <= 3; id++ {
		sid := SessionID(id.wireName())
		messages := h.messagesFor(sid)
		if len(messages) != 3 || messages[0].Type != "phase" || messages[1].Type != "status" || messages[2].Type != "summary" {
			t.Fatalf("wrong finish sequence: %v", messages)
		}
		data := h.summary(sid)
		want := 1
		if id == 3 {
			want = 3
		}
		if !data.Eligible || data.Rank == nil || *data.Rank != want || data.Score != messages[1].Data.(StatusData).Score {
			t.Fatalf("bad summary: %+v", data)
		}
		h.mu.Lock()
		sync := h.generation[sid]
		h.mu.Unlock()
		if data.Sync != sync {
			t.Fatal("SUMMARY generation does not match RESET")
		}
	}
	snapshot := h.state()
	*snapshot.Nodes[0].Rank = 8
	if *h.state().Nodes[0].Rank != 1 {
		t.Fatal("Snapshot rank aliases game state")
	}
	h.clear()
	h.tick(50 * time.Second)
	if len(h.messages()) != 0 {
		t.Fatal("results were periodically resent")
	}

	h.connect("P4", "000000000004", 4)
	unranked := h.summary("P4")
	encoded, err := json.Marshal(unranked)
	h.must(err)
	if unranked.Eligible || unranked.Rank != nil || unranked.Score != 1000 || !strings.Contains(string(encoded), `"rank":null`) {
		t.Fatalf("ineligible result: %s", encoded)
	}
	h.execute(Command{Operation: SetNodeEnabled, Node: 4, Enabled: true})
	if h.summary("P4").Eligible || h.state().Nodes[3].RankingEligible {
		t.Fatal("Finished mode change restored qualification")
	}

	h.clear()
	oldSync := h.currentSync("P2")
	h.message("P2", IncomingMessage{Type: "resync"})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"reset", "phase", "status", "summary"}) {
		t.Fatal(got)
	}
	next := h.summary("P2")
	if next.Sync == oldSync || next.Sync != h.reset("P2").Sync {
		t.Fatal("resync did not rebind SUMMARY")
	}

	h.clear()
	h.message("P2", IncomingMessage{Type: "send", Sync: next.Sync, RID: 1, PID: 65535, To: 1})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"result", "reset", "phase", "status", "summary"}) {
		t.Fatalf("Finished rejection missed result synchronization: %v", got)
	}
	if h.summary("P2").Sync != h.reset("P2").Sync {
		t.Fatal("rejection retained stale result generation")
	}
	h.clear()
	h.ready("P2")
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"status"}) {
		t.Fatal(got)
	}

	h.execute(Command{Operation: SwapDeviceAssignments, DeviceID: "000000000001", OtherDeviceID: "000000000004"})
	if data := h.summary("P1"); data.Eligible || data.Rank != nil || data.Score != 1000 {
		t.Fatal("swapped device received previous owner's result")
	}
	if data := h.summary("P4"); !data.Eligible || data.Rank == nil || *data.Rank != 1 || data.Score != 100 {
		t.Fatal("swap changed frozen node results")
	}
}

func (h *harness) currentSync(id SessionID) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.generation[id]
}

func TestPreviewUsesCanonicalFIFOAndRestoresResult(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.start()
	h.tick(time.Second)
	h.execute(Command{Operation: Finish})
	before := h.state()
	original := h.summary("p1")
	h.clear()
	h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"config", "reset", "phase", "status"}) {
		t.Fatal(got)
	}
	if !h.messages()[0].message.Data.(ConfigData).Preview {
		t.Fatal("preview configuration omitted")
	}
	if len(h.reset("p1").Packets) != before.Nodes[0].FIFOUsage {
		t.Fatal("preview replaced canonical FIFO")
	}
	h.clear()
	h.execute(Command{Operation: SetDevicePreview})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"config", "reset", "phase", "status", "summary"}) {
		t.Fatal(got)
	}
	if h.messages()[0].message.Data.(ConfigData).Preview {
		t.Fatal("preview flag was not cleared")
	}
	restored := h.summary("p1")
	if restored.Sync == original.Sync || restored.Score != original.Score || restored.Eligible != original.Eligible {
		t.Fatal("preview recovery changed the frozen result")
	}
	h.execute(Command{Operation: SetDevicePreview, DeviceID: testUID})
	h.clear()
	h.execute(Command{Operation: Reset})
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"config", "phase", "reset", "status"}) {
		t.Fatal(got)
	}
	if h.messages()[0].message.Data.(ConfigData).Preview || len(h.reset("p1").Packets) != 0 {
		t.Fatal("Reset did not clear preview and FIFO")
	}
}

func TestLEDUnassignmentAndUnassignedPreviewUseIdentify(t *testing.T) {
	h := newHarness(t)
	uid := "A00000000001"
	h.connectLED("led", uid, 9)
	h.clear()
	h.execute(Command{Operation: SetDeviceAssignment, Kind: LEDDevice, DeviceID: uid})
	if got := h.lines("led"); !reflect.DeepEqual(got, []string{`{"type":"identify","data":{}}`}) {
		t.Fatal(got)
	}
	h.message("led", IncomingMessage{Type: "hello", UID: uid, Kind: LEDDevice})
	if h.device(uid).Node != 0 {
		t.Fatal("unassigned LED was auto-assigned again")
	}
	for _, stop := range []Operation{SetDevicePreview, Reset} {
		h.execute(Command{Operation: SetDevicePreview, DeviceID: uid})
		h.clear()
		h.execute(Command{Operation: stop})
		if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"identify"}) {
			t.Fatalf("unassigned preview stop: %v", got)
		}
		h.message("led", IncomingMessage{Type: "hello", UID: uid, Kind: LEDDevice})
	}
}

func TestStructuredRefusalsAndStartBlockers(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.connect("p2", "112233AABBCC", 2)
	h.connectLED("led", "A00000000001", 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.execute(Command{Operation: SetNodeEnabled, Node: 2, Enabled: true})
	state := h.state()
	if !reflect.DeepEqual(state.StartBlockers, []StartBlocker{{Code: CodePlayersNotReady, Nodes: []NodeID{1, 2}}}) {
		t.Fatalf("bad blockers: %+v", state.StartBlockers)
	}
	refusal := expectCode(t, h.a.Execute(h.ctx, Command{Operation: Start}), CodePlayersNotReady)
	if !reflect.DeepEqual(refusal.Nodes, []NodeID{1, 2}) {
		t.Fatal("refusal lacks unready players")
	}
	state.StartBlockers[0].Nodes[0] = 8
	if h.state().StartBlockers[0].Nodes[0] != 1 {
		t.Fatal("blockers alias state")
	}
	for _, tc := range []struct {
		command Command
		code    Code
	}{
		{Command{Operation: Finish}, CodePhaseConflict},
		{Command{Operation: "unknown"}, CodeInvalid},
		{Command{Operation: SetDeviceAssignment, DeviceID: "unknown"}, CodeNotFound},
		{Command{Operation: SetNodeEnabled, Node: 3, Enabled: true}, CodeDeviceDisconnected},
		{Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: PlayerDevice, Node: 2}, CodeAssignmentConflict},
		{Command{Operation: SetDeviceAssignment, DeviceID: testUID, Kind: LEDDevice, Node: 3}, CodeDeviceKindMismatch},
		{Command{Operation: SetDevicePreview, DeviceID: testUID}, CodeManualModeConflict},
		{Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: testUID}, CodeInvalid},
		{Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: "A00000000001"}, CodeDeviceKindMismatch},
	} {
		expectCode(t, h.a.Execute(h.ctx, tc.command), tc.code)
	}
	h.execute(Command{Operation: SetDeviceAssignment, DeviceID: "112233AABBCC"})
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: SwapDeviceAssignments, DeviceID: testUID, OtherDeviceID: "112233AABBCC"}), CodeDeviceNotAssigned)
	h.must(h.a.Disconnected(h.ctx, "p2"))
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: ReplaceDeviceAssignment, DeviceID: testUID, OtherDeviceID: "112233AABBCC"}), CodeDeviceDisconnected)
	h.must(h.a.submit(h.ctx, false, func(time.Time) error { h.a.game.nextPID = 65536 - 999; return nil }))
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: Start}), CodePIDExhausted)
	h.must(h.a.submit(h.ctx, false, func(time.Time) error { h.a.game.nextPID = 0; return nil }))
	h.start()
	h.execute(Command{Operation: Finish})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: false})
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.must(h.a.submit(h.ctx, false, func(time.Time) error { h.a.game.nextPID = 65536 - 999; return nil }))
	expectCode(t, h.a.Execute(h.ctx, Command{Operation: Start}), CodePhaseConflict)
	blockers := h.state().StartBlockers
	if len(blockers) != 3 || blockers[0].Code != CodeNotPreparing || blockers[1].Code != CodePIDExhausted || blockers[2].Code != CodePlayersNotReady {
		t.Fatalf("blocker priority: %v", blockers)
	}
	h.cancel()
	<-h.a.done
	if _, err := h.a.State(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped app returned %v", err)
	}
}

func TestLEDIdentityRequiresUppercaseTwelveHexDigits(t *testing.T) {
	for _, uid := range []string{"LED-1", "aabbccddeeff", "AABBCCDDEEF", "AABBCCDDEEFF0", "AABBCCDDEEFG"} {
		if validIdentity(LEDDevice, uid) {
			t.Fatalf("invalid LED UID accepted: %q", uid)
		}
	}
	h := newHarness(t)
	h.must(h.a.Connected(h.ctx, "led"))
	h.message("led", IncomingMessage{Type: "hello", Kind: LEDDevice, UID: "LED-1"})
	if len(h.state().Devices) != 0 {
		t.Fatal("invalid HELLO created a device")
	}
	h.tick(time.Second)
	if got := messageTypes(h.messages()); !reflect.DeepEqual(got, []string{"identify", "identify"}) {
		t.Fatal(got)
	}
	h.message("led", IncomingMessage{Type: "hello", Kind: LEDDevice, UID: "AABBCCDDEEFF"})
	if len(h.state().Devices) != 1 {
		t.Fatal("valid LED identity rejected")
	}
}

func TestSummaryFailurePreservesResultForReconnect(t *testing.T) {
	h := newHarness(t)
	h.connect("p1", testUID, 1)
	h.execute(Command{Operation: SetNodeEnabled, Node: 1, Enabled: true})
	h.start()
	originalSync := h.currentSync("p1")
	h.mu.Lock()
	h.failType = "summary"
	h.mu.Unlock()
	h.execute(Command{Operation: Finish})
	result := h.state().Nodes[0]
	if h.device(testUID).Connected || !result.RankingEligible || result.Rank == nil || *result.Rank != 1 {
		t.Fatal("failed result notification changed final qualification")
	}
	h.mu.Lock()
	h.failType = ""
	h.mu.Unlock()
	h.must(h.a.Connected(h.ctx, "p1-new"))
	h.message("p1-new", IncomingMessage{Type: "hello", UID: testUID})
	data := h.summary("p1-new")
	if data.Sync == originalSync || !data.Eligible || data.Rank == nil || *data.Rank != 1 || data.Score != result.Score {
		t.Fatalf("reconnection did not restore frozen result: %+v", data)
	}
	if h.state().Nodes[0].Enabled {
		t.Fatal("reconnection restored manual control")
	}
}

func TestLEDResetOnlyReconfiguresPreviewTarget(t *testing.T) {
	h := newHarness(t)
	h.connectLED("first", "A00000000001", 9)
	h.connectLED("second", "A00000000002", 10)
	h.execute(Command{Operation: SetDevicePreview, DeviceID: "A00000000001"})
	h.clear()
	h.execute(Command{Operation: SetDeviceAssignment, Kind: LEDDevice, DeviceID: "A00000000002", Node: 11})
	if h.state().Preview == nil || len(h.lines("first")) != 0 {
		t.Fatal("unrelated assignment canceled LED preview")
	}
	h.clear()
	h.execute(Command{Operation: Reset})
	if got := h.lines("first"); !reflect.DeepEqual(got, []string{"CONFIG B1 false", "BUFFER 0"}) {
		t.Fatal(got)
	}
	if got := h.lines("second"); !reflect.DeepEqual(got, []string{"CLEAR", "BUFFER 0"}) {
		t.Fatal(got)
	}
	if h.state().Preview != nil {
		t.Fatal("Reset kept the preview target")
	}
}
