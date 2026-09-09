package video

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// FPS is how often the camera is sampled and, therefore, how often a frame is
// sent to each peer.
//
// Ten is not a compromise on quality so much as a reading of what the medium
// can show: a 40-column tile has already thrown away most of the detail, and
// motion at ten frames reads as fluid in ASCII long before it would in a real
// picture. It also sets the bill -- every frame is a message to every peer,
// and doubling this doubles a call's upstream.
const FPS = 10

// Camera is a running ffmpeg, handing back frames until it is closed.
type Camera struct {
	cmd    *exec.Cmd
	frames chan *Frame
	stderr *strings.Builder
	closed sync.Once
	cancel context.CancelFunc

	mu  sync.Mutex
	err error
}

// OpenCamera starts capturing. The returned Camera must be closed, or ffmpeg
// outlives the program holding the webcam light on.
func OpenCamera(ctx context.Context) (*Camera, error) {
	bin, err := FFmpeg()
	if err != nil {
		return nil, err
	}
	dev, err := defaultDevice(ctx, bin)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	args := append(dev.input(),
		"-an",
		// A webcam is usually 16:9 and the wire format is 4:3. Scaling to fit
		// would squash faces, so the picture is scaled until it covers and
		// then centre-cropped -- the same choice a video call UI makes.
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
			WireW, WireH, WireW, WireH),
		"-r", strconv.Itoa(FPS),
		"-pix_fmt", "rgb24",
		"-f", "rawvideo",
		"-",
	)

	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var errbuf strings.Builder
	cmd.Stderr = &limitedWriter{w: &errbuf, n: 8 << 10}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("could not start ffmpeg: %w", err)
	}

	c := &Camera{
		cmd:    cmd,
		frames: make(chan *Frame, 2),
		stderr: &errbuf,
		cancel: cancel,
	}
	go c.read(stdout)
	return c, nil
}

// read turns the raw byte stream into frames.
//
// The channel is small and a frame is dropped when it is full. Buffering
// instead would trade the one thing a call cannot spare -- being current --
// for frames nobody will ever see, and the backlog only ever grows.
func (c *Camera) read(r io.Reader) {
	defer close(c.frames)
	br := bufio.NewReaderSize(r, WireW*WireH*3*2)
	for {
		f := NewFrame(WireW, WireH)
		if _, err := io.ReadFull(br, f.Pix); err != nil {
			c.mu.Lock()
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				c.err = err
			}
			c.mu.Unlock()
			return
		}
		select {
		case c.frames <- f:
		default:
		}
	}
}

func (c *Camera) Frames() <-chan *Frame { return c.frames }

// Err explains why capture stopped. ffmpeg says why on stderr and then exits,
// so its last words are worth more than the exit status.
func (c *Camera) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	if msg := lastLine(c.stderr.String()); msg != "" {
		return fmt.Errorf("ffmpeg: %s", msg)
	}
	return nil
}

func (c *Camera) Close() {
	c.closed.Do(func() {
		c.cancel()
		_ = c.cmd.Wait()
	})
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" && !strings.HasPrefix(l, "frame=") {
			return l
		}
	}
	return ""
}

// limitedWriter keeps the first n bytes and drops the rest. ffmpeg narrates
// every frame it encodes; without a cap that log grows for the length of the
// call, to say nothing new after the first second.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	n, err := l.w.Write(p)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

// device is one camera, described the way this platform's ffmpeg wants it.
type device struct {
	name string   // what to show a person
	args []string // the -f/-i pair that opens it
}

func (d device) input() []string { return d.args }
