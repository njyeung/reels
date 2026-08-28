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

// Frame opcodes
const (
	opHandshake uint32 = 0
	opFrame     uint32 = 1
	opClose     uint32 = 2
	opPing      uint32 = 3
	opPong      uint32 = 4
)

const (
	discordAppID = "1542825678968201216"

	// redialInterval is how long to wait before hunting for the socket again.
	redialInterval = 15 * time.Second

	// maxPayload bounds the length header
	maxPayload = 64 * 1024

	// activity update interval
	minUpdateInterval = 5 * time.Second
)

var (
	mu sync.Mutex

	details string
	state   string
	reelURL string
)

var startTime time.Time

func SetDetails(d string) {
	mu.Lock()
	details = d
	mu.Unlock()
}

func SetReelURL(r string) {
	mu.Lock()
	reelURL = r
	mu.Unlock()
}

func SetState(s string) {
	mu.Lock()
	state = s
	mu.Unlock()
}

func init() {
	startTime = time.Now()
	go run()
}

func run() {
	for {
		conn, err := connectToDiscord()
		if err != nil {
			time.Sleep(redialInterval)
			continue
		}

		serveConnection(conn)
		_ = conn.Close()

		time.Sleep(redialInterval)
	}
}

type handshake struct {
	V        int    `json:"v"`
	ClientID string `json:"client_id"`
}

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

func connectToDiscord() (net.Conn, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}

	if err := writeFrame(conn, opHandshake, handshake{V: 1, ClientID: discordAppID}); err != nil {
		_ = conn.Close()
		return nil, err
	}

	op, payload, err := readFrame(conn)
	if err != nil || op != opFrame {
		_ = conn.Close()
		return nil, err
	}

	var ready struct {
		Evt string `json:"evt"`
	}
	if err := json.Unmarshal(payload, &ready); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ready.Evt != "READY" {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected handshake response")
	}

	return conn, nil
}

type incoming struct {
	op      uint32
	payload []byte
	err     error
}

func serveConnection(conn net.Conn) {
	frames := make(chan incoming)

	go func() {
		for {
			op, payload, err := readFrame(conn)
			frames <- incoming{op, payload, err}
		}
	}()

	go func() {
		for {
			writeFrame(conn, opFrame, setActivity())
			time.Sleep(minUpdateInterval)
		}
	}()

	for frame := range frames {
		if frame.err != nil {
			return
		}
		if frame.op == opPing {
			if err := writeRaw(conn, opPong, frame.payload); err != nil {
				return
			}
		}
	}
}

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

type command struct {
	Cmd   string `json:"cmd"`
	Nonce string `json:"nonce"`
	Args  args   `json:"args"`
}

type args struct {
	PID      int      `json:"pid"`
	Activity activity `json:"activity"`
}

type activity struct {
	Type       int        `json:"type"`
	Details    string     `json:"details,omitempty"`
	State      string     `json:"state,omitempty"`
	Timestamps timestamps `json:"timestamps"`
	Assets     assets     `json:"assets"`
	Buttons    []button   `json:"buttons,omitempty"`
}

type timestamps struct {
	Start int64 `json:"start"`
}

type assets struct {
	LargeImage string `json:"large_image"`
	LargeText  string `json:"large_text"`
}

type button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// setActivity builds the frame that Discord renders as the presence card
func setActivity() command {
	mu.Lock()
	d := details
	s := state
	r := reelURL
	mu.Unlock()

	var buttons []button
	if r != "" {
		buttons = append(buttons, button{
			Label: "View Reel",
			URL:   r,
		})
	}
	buttons = append(buttons, button{
		Label: "GitHub",
		URL:   "https://github.com/njyeung/reels",
	})

	return command{
		Cmd:   "SET_ACTIVITY",
		Nonce: strconv.FormatInt(time.Now().UnixNano(), 10),
		Args: args{
			PID: os.Getpid(),
			Activity: activity{
				Type:       3,
				Details:    d,
				State:      s,
				Timestamps: timestamps{Start: startTime.Unix()},
				Assets: assets{
					LargeImage: "instagram",
					LargeText:  "Instagram reels in the terminal",
				},
				Buttons: buttons,
			},
		},
	}
}
