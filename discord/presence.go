// Package discord publishes a Rich Presence activity to a Discord client
// running on the same machine, over the local IPC socket it listens on.
package discord

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Frame opcodes.
const (
	opHandshake uint32 = 0
	opFrame     uint32 = 1
	opClose     uint32 = 2
	opPing      uint32 = 3
	opPong      uint32 = 4
)

const (
	// redialInterval is how long to wait before hunting for the socket again.
	redialInterval = 15 * time.Second

	// maxPayload bounds the length header
	maxPayload = 64 * 1024
)

// Presence holds a Rich Presence activity on a Discord client for as long as
// the process lives.
type Presence struct {
	done chan struct{}
	once sync.Once
}

func Start(appID string) *Presence {
	p := &Presence{done: make(chan struct{})}
	go p.run(appID, time.Now())
	return p
}

// Close clears the activity by hanging up. Discord drops a presence as soon as
// its socket closes, so there is no "unset" call to make.
func (p *Presence) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() { close(p.done) })
}

// run reconnects for the life of the process. Each pass holds one connection
// open until it dies, so the activity survives as long as the client does.
func (p *Presence) run(appID string, since time.Time) {
	for {
		conn, err := dial()
		if err == nil {
			p.session(conn, appID, since)
		}

		select {
		case <-p.done:
			return
		case <-time.After(redialInterval):
		}
	}
}

// session runs one connection: handshake, publish, then sit on the socket
// answering pings until it drops or Close is called.
func (p *Presence) session(conn net.Conn, appID string, since time.Time) {
	defer conn.Close()

	// Closing the socket is what unblocks the read loop below, since a read on
	// a live connection parks indefinitely waiting for Discord to say something.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-p.done:
			conn.Close()
		case <-stop:
		}
	}()

	if err := writeFrame(conn, opHandshake, handshake{V: 1, ClientID: appID}); err != nil {
		return
	}

	// Discord answers a handshake with a READY dispatch. Anything else means it
	// refused the app id, and every activity sent afterwards would be dropped on
	// the floor without further complaint.
	op, payload, err := readFrame(conn)
	if err != nil {
		return
	}
	var ready struct {
		Evt string `json:"evt"`
	}
	json.Unmarshal(payload, &ready)
	if op != opFrame || ready.Evt != "READY" {
		return
	}

	if err := writeFrame(conn, opFrame, setActivity(since)); err != nil {
		return
	}

	for {
		op, payload, err := readFrame(conn)
		if err != nil {
			return
		}
		if op == opPing {
			if err := writeRaw(conn, opPong, payload); err != nil {
				return
			}
		}
	}
}

// dial finds a Discord client's IPC socket. Discord numbers the socket 0-9 so
// that stable, PTB and canary can run side by side, and the sandboxed builds
// bury it a directory down from the runtime dir.
func dial() (net.Conn, error) {
	var roots []string
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if dir := os.Getenv(env); dir != "" {
			roots = append(roots, dir)
		}
	}
	roots = append(roots, "/tmp")

	dirs := make([]string, 0, len(roots)*3)
	for _, root := range roots {
		dirs = append(dirs, root,
			filepath.Join(root, "app", "com.discordapp.Discord"), // flatpak
			filepath.Join(root, "snap.discord"),                  // snap
		)
	}

	for _, dir := range dirs {
		for i := range 10 {
			path := filepath.Join(dir, "discord-ipc-"+strconv.Itoa(i))
			if conn, err := net.DialTimeout("unix", path, 2*time.Second); err == nil {
				return conn, nil
			}
		}
	}
	return nil, fmt.Errorf("no discord ipc socket found")
}

// A frame is a little-endian opcode and length followed by that many bytes of JSON.
func writeFrame(conn net.Conn, op uint32, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeRaw(conn, op, payload)
}

func writeRaw(conn net.Conn, op uint32, payload []byte) error {
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], op)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	copy(buf[8:], payload)

	_, err := conn.Write(buf)
	return err
}

func readFrame(conn net.Conn) (uint32, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return 0, nil, err
	}

	op := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])
	if length > maxPayload {
		return 0, nil, fmt.Errorf("discord frame of %d bytes is too large", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return op, payload, nil
}

type handshake struct {
	V        int    `json:"v"`
	ClientID string `json:"client_id"`
}

type command struct {
	Cmd   string `json:"cmd"`
	Nonce string `json:"nonce"`
	Args  args   `json:"args"`
}

type args struct {
	// PID is the process Discord watches to know when to clear the presence,
	// so a crash does not leave the user reading reels forever.
	PID      int      `json:"pid"`
	Activity activity `json:"activity"`
}

type activity struct {
	Details    string     `json:"details,omitempty"`
	State      string     `json:"state,omitempty"`
	Timestamps timestamps `json:"timestamps"`
	Assets     assets     `json:"assets"`
	Buttons    []button   `json:"buttons,omitempty"`
}

type timestamps struct {
	// Start turns the card into a live elapsed timer, counting up from here.
	Start int64 `json:"start"`
}

// Image fields name art assets uploaded to the application in Discord's
// developer portal, not URLs. The images never ship in the binary.
type assets struct {
	LargeImage string `json:"large_image"`
	LargeText  string `json:"large_text"`
}

type button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// setActivity builds the frame that Discord renders as the presence card. The
// "Playing REELS TUI" headline is not in here: that is the application's name
// in the developer portal, which the client resolves from the app id.
func setActivity(since time.Time) command {
	return command{
		Cmd: "SET_ACTIVITY",
		// Discord only uses the nonce to match replies to commands, and we
		// send one command per connection.
		Nonce: strconv.FormatInt(since.UnixNano(), 10),
		Args: args{
			PID: os.Getpid(),
			Activity: activity{
				Details:    "doomscrolling",
				Timestamps: timestamps{Start: since.Unix()},
				Assets: assets{
					LargeImage: "instagram",
					LargeText:  "Instagram reels in the terminal",
				},
				Buttons: []button{{
					Label: "View on GitHub",
					URL:   "https://github.com/njyeung/reels",
				}},
			},
		},
	}
}
