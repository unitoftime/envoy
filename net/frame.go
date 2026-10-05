package net

import (
	// "fmt"
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"time"

	// TODO - Implement my own framing?

	"github.com/smallnest/goframe"
)

const frameHeaderSize = 4

// Splits a byte stream into messages by prefixing each one with its length, the same wire format as FrameConn.
// Messages are read straight into the caller's buffer, so nothing is allocated per message and a peer can't make us allocate by claiming a huge length.
// Note: Reads aren't safe to run concurrently with other reads, nor writes with other writes. PipeSocket serializes both
type framer struct {
	r *bufio.Reader
	w *bufio.Writer
	readHeader, writeHeader [frameHeaderSize]byte
}

func newFramer(stream io.ReadWriter) *framer {
	return &framer{
		r: bufio.NewReader(stream),
		w: bufio.NewWriter(stream),
	}
}

// Reads the next message into b. A message that doesn't fit is truncated to len(b)
func (f *framer) Read(b []byte) (int, error) {
	_, err := io.ReadFull(f.r, f.readHeader[:])
	if err != nil {
		return 0, err
	}
	length := int64(binary.BigEndian.Uint32(f.readHeader[:]))

	n := int(min(length, int64(len(b))))
	_, err = io.ReadFull(f.r, b[:n])
	if err != nil {
		return 0, err
	}

	if length > int64(n) {
		// Skip what didn't fit, so that the next read starts on a header
		_, err = io.CopyN(io.Discard, f.r, length - int64(n))
		if err != nil {
			return 0, err
		}
	}

	return n, nil
}

func (f *framer) Write(b []byte) (int, error) {
	if uint64(len(b)) > math.MaxUint32 {
		return 0, errors.New("envoy: message is too large to frame")
	}
	binary.BigEndian.PutUint32(f.writeHeader[:], uint32(len(b)))

	// Note: The header and message are buffered together so that small messages reach the stream as a single write
	_, err := f.w.Write(f.writeHeader[:])
	if err != nil {
		return 0, err
	}
	_, err = f.w.Write(b)
	if err != nil {
		return 0, err
	}
	err = f.w.Flush()
	if err != nil {
		return 0, err
	}

	return len(b), nil
}

type FrameConn struct {
	frameConn goframe.FrameConn
	conn net.Conn
}

func NewFrameConn(conn net.Conn) *FrameConn {
	encCfg := goframe.EncoderConfig{
		ByteOrder: binary.BigEndian,
		LengthFieldLength: 4, // Note: 2 byte length, maximum 64k
	}
	decCfg := goframe.DecoderConfig{
		ByteOrder: binary.BigEndian,
		LengthFieldLength: 4, // Note: 2 byte length, maximum 64k
		InitialBytesToStrip: 4, // Note: Strip out the first two bytes (ie the lenghtField)
	}

	return &FrameConn{
		frameConn: goframe.NewLengthFieldBasedFrameConn(encCfg, decCfg, conn),
		conn: conn,
	}
}

func (f *FrameConn) Read(b []byte) (int, error) {
	// TODO - hack because goframe creates a buffer for reads. Unless I want to allocate on every read I need to eventually have it pass in else it might waste memory. I'm just going to copy. Or actually it might be more efficient to have the connection manage the buffers then just return those with the expectation that they'll be finished being used before the next read (or something)
	tmpBuf, err := f.frameConn.ReadFrame()
	if err != nil {
		return 0, err // TODO - Assuming I return 0 length here? I guess I'm not sure. Maybe we have read some values?
	}

	length := copy(b, tmpBuf)
	// if length >= len(b) {
	// 	log.Error().
	// 		Int("Receved", len(tmpBuf)).
	// 		Int("Expected", len(b)).
	// 		Msg("Envoy.Net.FrameConn: Received message larger than read buffer")
	// }

	return length, nil
}

func (f *FrameConn) Write(dat []byte) (int, error) {
	return len(dat), f.frameConn.WriteFrame(dat)
}

func (f *FrameConn) Close() error {
	return f.frameConn.Close()
}

func (f *FrameConn) LocalAddr() net.Addr {
	return f.conn.LocalAddr()
}

func (f *FrameConn) RemoteAddr() net.Addr {
	return f.conn.RemoteAddr()
}

func (f *FrameConn) SetDeadline(t time.Time) error {
	return f.conn.SetDeadline(t)
}

func (f *FrameConn) SetReadDeadline(t time.Time) error {
	return f.conn.SetReadDeadline(t)
}

func (f *FrameConn) SetWriteDeadline(t time.Time) error {
	return f.conn.SetWriteDeadline(t)
}
