package base

import (
	"io"
	"sync"
)

type Emitter struct {
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	closed     bool
	mutex      sync.RWMutex
}

func (em *Emitter) IsClosed() bool {
	em.mutex.RLock()
	defer em.mutex.RUnlock()
	return em.closed
}

func (em *Emitter) Read(b []byte) (int, error) {
	em.mutex.RLock()
	if em.closed {
		em.mutex.RUnlock()
		return 0, io.EOF
	}
	em.mutex.RUnlock()
	return em.pipeReader.Read(b)
}

func (em *Emitter) Write(b []byte) (int, error) {
	em.mutex.RLock()
	if em.closed {
		em.mutex.RUnlock()
		return 0, io.ErrClosedPipe
	}
	em.mutex.RUnlock()
	return em.pipeWriter.Write(b)
}

func (em *Emitter) WriteString(s string) (int, error) {
	return em.Write([]byte(s))
}

func (em *Emitter) Close() error {
	em.mutex.Lock()
	defer em.mutex.Unlock()
	if em.closed {
		return nil // 已经关闭，直接返回
	}
	em.closed = true
	em.pipeReader.Close()
	em.pipeWriter.Close()
	return nil
}

func NewEmitter(reader *io.PipeReader, writer *io.PipeWriter) *Emitter {
	return &Emitter{
		pipeReader: reader,
		pipeWriter: writer,
		closed:     false,
	}
}
