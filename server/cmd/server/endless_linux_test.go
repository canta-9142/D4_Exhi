package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"
)

// endlessMCU speaks the player protocol to the server over a pseudo-TTY, as
// the firmware does (player.md). It applies the FIFO that the running network
// keeps sending, so that a test waits only for the messages it checks.
type endlessMCU struct {
	t      *testing.T
	writer io.Writer
	reader *bufio.Reader
	sync   string
	queue  []map[string]int
	status endlessStatus
}

type endlessStatus struct {
	Score, Remaining, Countdown, Window, Combo int
	Ready                                      bool
	Round                                      *string
	Rank                                       *int
	Gains                                      []int
	Bots                                       []int
}

func (m *endlessMCU) write(kind string, data any) {
	m.t.Helper()
	line, err := json.Marshal(map[string]any{"type": kind, "data": data})
	if err != nil {
		m.t.Fatal(err)
	}
	if _, err := m.writer.Write(append(line, '\n')); err != nil {
		m.t.Fatal(err)
	}
}

func (m *endlessMCU) decode(data json.RawMessage, v any) {
	m.t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		m.t.Fatalf("%s: %v", data, err)
	}
}

// next returns the data of the next message of kind. reset, push, result and
// status are applied on the way; any other message is a failure.
func (m *endlessMCU) next(kind string) json.RawMessage {
	m.t.Helper()
	for {
		line, err := m.reader.ReadBytes('\n')
		if err != nil {
			m.t.Fatalf("waiting for %s: %v", kind, err)
		}
		var message struct {
			Type string
			Data json.RawMessage
		}
		m.decode(line, &message)
		switch message.Type {
		case "reset":
			var data struct {
				Sync    string
				Packets []map[string]int
			}
			m.decode(message.Data, &data)
			m.sync, m.queue = data.Sync, data.Packets
		case "push":
			var p map[string]int
			m.decode(message.Data, &p)
			m.queue = append(m.queue, p)
		case "result":
			var data struct{ OK bool }
			m.decode(message.Data, &data)
			if data.OK {
				m.queue = m.queue[1:]
			}
		case "status":
			m.status = endlessStatus{}
			m.decode(message.Data, &m.status)
		default:
			if message.Type != kind {
				m.t.Fatalf("got %s %s while waiting for %s", message.Type, message.Data, kind)
			}
		}
		if message.Type == kind {
			return message.Data
		}
	}
}

// phase waits for the next phase, which must be want, and the status that
// follows it.
func (m *endlessMCU) phase(want string) endlessStatus {
	m.t.Helper()
	var data struct{ Phase string }
	m.decode(m.next("phase"), &data)
	if data.Phase != want {
		m.t.Fatalf("phase %s, want %s", data.Phase, want)
	}
	m.next("status")
	return m.status
}

func (m *endlessMCU) config(want string) {
	m.t.Helper()
	var data struct {
		Node    int
		Mode    string
		Preview bool
	}
	m.decode(m.next("config"), &data)
	if data.Node != 1 || data.Mode != want || data.Preview {
		m.t.Fatalf("config %+v, want P1 %s", data, want)
	}
}

// TestRunEndlessStationOverPTY runs one station of endless operation through
// main, its serial port and the admin API: the station is stopped, becomes
// manual, idles once operation starts, enters the mode selection on a long
// press, practices, leaves the practice on a long press, and is cut short by
// the switch to automatic. The timed parts of rounds are app's tests.
func TestRunEndlessStationOverPTY(t *testing.T) {
	player, playerPath := serverTestPTY(t)
	if err := player.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	var output syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-http-addr", testAddr, "-devices-file", filepath.Join(dir, "devices.json"),
			"-records-file", filepath.Join(dir, "records.sqlite3"), "-serial-port", playerPath}, &output)
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	}()

	mcu := &endlessMCU{t: t, writer: player, reader: bufio.NewReader(player)}
	mcu.next("identify")
	mcu.write("hello", map[string]string{"uid": "001122AABBCC"})
	mcu.config("auto")
	if s := mcu.phase("Preparing"); s.Round != nil || s.Window != 0 || s.Bots != nil {
		t.Fatalf("stopped status %+v", s)
	}
	addr := listenAddr(t, &output)
	postCommand(t, addr, `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"manual"}}`)
	mcu.config("manual")
	mcu.phase("Preparing")

	// Start, then press both ends before the first packet is forwarded (1.5 s),
	// so that no reset of the idle FIFO overtakes the request.
	postCommand(t, addr, `{"type":"start","data":{}}`)
	if s := mcu.phase("Idle"); s.Round != nil || s.Window != 0 || len(mcu.queue) != 0 {
		t.Fatalf("idle status %+v, queue %v", s, mcu.queue)
	}
	mcu.write("ready", map[string]string{"sync": mcu.sync})
	if s := mcu.phase("Select"); s.Window != 20 || s.Round != nil {
		t.Fatalf("select status %+v", s)
	}
	mcu.write("choose", map[string]string{"sync": mcu.sync, "round": "tutorial"})
	s := mcu.phase("Playing")
	if s.Round == nil || *s.Round != "tutorial" || s.Countdown != 3 || s.Window != 60 || len(s.Bots) != 3 || len(mcu.queue) != 3 {
		t.Fatalf("practice status %+v, queue %v", s, mcu.queue)
	}
	for mcu.status.Countdown != 0 {
		mcu.next("status")
	}

	// A practice SEND is answered from the practice queue, which stays at 3.
	head := mcu.queue[0]["pid"]
	mcu.write("send", map[string]any{"sync": mcu.sync, "rid": 0, "pid": head, "to": 2})
	var result struct {
		Sync string
		RID  int
		OK   bool
	}
	mcu.decode(mcu.next("result"), &result)
	if result.Sync != mcu.sync || result.RID != 0 || !result.OK {
		t.Fatalf("practice result %+v", result)
	}
	mcu.next("push")
	if len(mcu.queue) != 3 {
		t.Fatalf("practice queue %v", mcu.queue)
	}

	// A long press leaves the practice: phase, the real FIFO's reset, status.
	practiceSync := mcu.sync
	mcu.write("ready", map[string]string{"sync": mcu.sync})
	if s := mcu.phase("Select"); s.Window != 20 || s.Bots != nil || mcu.sync == practiceSync {
		t.Fatalf("select after practice %+v", s)
	}

	// Switching to automatic cuts the selection short: config, reset, phase, status.
	postCommand(t, addr, `{"type":"set_node_mode","data":{"nodeId":"P1","mode":"auto"}}`)
	mcu.config("auto")
	if s := mcu.phase("Idle"); s.Window != 0 || s.Round != nil {
		t.Fatalf("idle after auto %+v", s)
	}
}
