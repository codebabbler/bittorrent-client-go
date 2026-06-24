package peer

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"
)

// OutstandingRequest tracks a block request sent to a peer.
type OutstandingRequest struct {
	Index int
	Begin int
	Len   int
	Sent  time.Time
}

// PeerMessage is the message format passed from Client loop to TorrentManager.
type PeerMessage struct {
	PeerAddress string
	ID          uint8  // 0xFF = error / disconnection, 0xFE = block timeout
	Payload     []byte
	PBuf        *[]byte // Pointer to buffer for sync.Pool recycling
}

// StartLoop initializes and starts the async reader, writer, and ticker loops.
func (c *Client) StartLoop(msgChan chan<- PeerMessage) {
	c.mu.Lock()
	c.LastRead = time.Now()
	c.LastWrite = time.Now()
	c.OutstandingRequests = make(map[string]OutstandingRequest)
	c.writeCh = make(chan []byte, 128)
	c.done = make(chan struct{})
	c.Choked = true // default state is choked
	c.mu.Unlock()

	// Drain any buffered messages we already read during handshake/init
	for _, bm := range c.bufferedMsgs {
		msgChan <- PeerMessage{
			PeerAddress: c.Address,
			ID:          bm.id,
			Payload:     bm.payload,
		}
	}
	c.bufferedMsgs = nil

	go c.readLoop(msgChan)
	go c.writeLoop()
	go c.tickerLoop(msgChan)
}

func (c *Client) readLoop(msgChan chan<- PeerMessage) {
	defer c.Close()
	for {
		id, payload, pBuf, err := readMessage(c.Conn)
		if err != nil {
			select {
			case msgChan <- PeerMessage{PeerAddress: c.Address, ID: 0xFF, Payload: []byte(err.Error())}:
			case <-c.done:
			}
			return
		}

		c.mu.Lock()
		c.LastRead = time.Now()
		c.mu.Unlock()

		if payload == nil {
			// keepalive - just update LastRead (already done above)
			continue
		}

		// Update state machine
		if id == MsgChoke {
			c.mu.Lock()
			c.Choked = true
			c.mu.Unlock()
		} else if id == MsgUnchoke {
			c.mu.Lock()
			c.Choked = false
			c.mu.Unlock()
		} else if id == MsgPiece {
			if len(payload) >= 8 {
				index := binary.BigEndian.Uint32(payload[0:4])
				begin := binary.BigEndian.Uint32(payload[4:8])
				key := fmt.Sprintf("%d_%d", index, begin)
				c.mu.Lock()
				delete(c.OutstandingRequests, key)
				c.mu.Unlock()
			}
		}

		select {
		case msgChan <- PeerMessage{PeerAddress: c.Address, ID: id, Payload: payload, PBuf: pBuf}:
		case <-c.done:
			if pBuf != nil {
				MessageBufferPool.Put(pBuf)
			}
			return
		}
	}
}

func (c *Client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.writeCh:
			_, err := c.Conn.Write(msg)
			if err != nil {
				c.Close()
				return
			}
			c.mu.Lock()
			c.LastWrite = time.Now()
			c.mu.Unlock()
		}
	}
}

func (c *Client) tickerLoop(msgChan chan<- PeerMessage) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()

			// 1. Inactivity timeout: close if silent for > 120s
			if now.Sub(c.LastRead) > 120*time.Second {
				c.mu.Unlock()
				fmt.Fprintf(os.Stderr, "Peer %s timed out due to inactivity\n", c.Address)
				c.Close()
				return
			}

			// 2. Keepalive: send if we haven't written for > 120s
			if now.Sub(c.LastWrite) > 120*time.Second {
				c.mu.Unlock()
				c.QueueKeepalive()
				c.mu.Lock()
			}

			// 3. Stall Monitor: check outstanding block requests
			var stalled []OutstandingRequest
			for key, req := range c.OutstandingRequests {
				if now.Sub(req.Sent) > 15*time.Second {
					stalled = append(stalled, req)
					delete(c.OutstandingRequests, key)
				}
			}
			c.mu.Unlock()

			if len(stalled) > 0 {
				c.mu.Lock()
				c.StallScore += len(stalled)
				tooManyStalls := c.StallScore > 3
				c.mu.Unlock()

				for _, req := range stalled {
					c.QueueCancel(req.Index, req.Begin, req.Len)

					// Dispatch a sentinel message so TorrentManager knows to return this block to piece queue
					payload := make([]byte, 8)
					binary.BigEndian.PutUint32(payload[0:4], uint32(req.Index))
					binary.BigEndian.PutUint32(payload[4:8], uint32(req.Begin))

					select {
					case msgChan <- PeerMessage{PeerAddress: c.Address, ID: 0xFE, Payload: payload}:
					case <-c.done:
						return
					}
				}

				if tooManyStalls {
					fmt.Fprintf(os.Stderr, "Peer %s stalled too many times, dropping\n", c.Address)
					c.Close()
					return
				}
			}
		}
	}
}

// QueueMessage packages and queues a BitTorrent protocol message to send.
func (c *Client) QueueMessage(id uint8, payload []byte) {
	msg := make([]byte, 4+1+len(payload))
	binary.BigEndian.PutUint32(msg[0:4], uint32(1+len(payload)))
	msg[4] = id
	copy(msg[5:], payload)

	select {
	case c.writeCh <- msg:
	case <-c.done:
	}
}

// QueueKeepalive queues a keepalive message (length-prefix 0).
func (c *Client) QueueKeepalive() {
	msg := make([]byte, 4)
	select {
	case c.writeCh <- msg:
	case <-c.done:
	}
}

// QueueRequest queues a block request message and starts tracking it.
func (c *Client) QueueRequest(index, begin, length int) {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))

	key := fmt.Sprintf("%d_%d", index, begin)
	c.mu.Lock()
	c.OutstandingRequests[key] = OutstandingRequest{
		Index: index,
		Begin: begin,
		Len:   length,
		Sent:  time.Now(),
	}
	c.mu.Unlock()

	c.QueueMessage(MsgRequest, payload)
}

// QueueCancel queues a cancel request message.
func (c *Client) QueueCancel(index, begin, length int) {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))

	c.QueueMessage(MsgCancel, payload)
}

// GetOutstandingCount returns the count of active block requests.
func (c *Client) GetOutstandingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.OutstandingRequests)
}

// IsChoked returns if the client is currently choked by the peer.
func (c *Client) IsChoked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Choked
}
