package net

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

const frameHeaderSize = 4

// Splits a byte stream into messages by prefixing each one with its length, as a 4 byte big endian header.
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
