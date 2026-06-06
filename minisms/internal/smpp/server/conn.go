// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"sync"

	"github.com/fiorix/go-smpp/v2/smpp/pdu"
)

type conn struct {
	rwc    net.Conn
	r      *bufio.Reader
	w      *bufio.Writer
	wmu    sync.Mutex
	remote net.Addr
}

func newConn(c net.Conn) *conn {
	return &conn{
		rwc:    c,
		r:      bufio.NewReader(c),
		w:      bufio.NewWriter(c),
		remote: c.RemoteAddr(),
	}
}

func (c *conn) RemoteAddr() net.Addr { return c.remote }

func (c *conn) Read() (pdu.Body, error) {
	return pdu.Decode(c.r)
}

func (c *conn) Write(p pdu.Body) error {
	var b bytes.Buffer
	if err := p.SerializeTo(&b); err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := io.Copy(c.w, &b); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *conn) Close() error {
	return c.rwc.Close()
}
