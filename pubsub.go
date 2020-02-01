package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// PubSub provides publish/subscribe HTTP service handlers.
type PubSub struct {
	mx          sync.RWMutex
	subscribers []*subscriber
}

// NewPubSub creates a new PubSub.
// buffer is how many published messages can be buffered before blocking.
func NewPubSub() *PubSub {
	return &PubSub{}
}

// Close cancels all subscribers, you should probably not accept new requests before this.
func (ps *PubSub) Close() error {
	ps.mx.Lock()
	defer ps.mx.Unlock()
	for _, sub := range ps.subscribers {
		sub.Close() // always nil
	}
	ps.subscribers = nil
	return nil
}

func (ps *PubSub) addSubscriber(sub *subscriber) {
	ps.mx.Lock()
	defer ps.mx.Unlock()
	ps.subscribers = append(ps.subscribers, sub)
}

func (ps *PubSub) removeSubscriber(sub *subscriber) bool {
	ps.mx.Lock()
	defer ps.mx.Unlock()
	for i, xsub := range ps.subscribers {
		if sub == xsub {
			// Remove it.
			ps.subscribers = append(ps.subscribers[:i], ps.subscribers[i+1:]...)
			sub.Close()
			return true
		}
	}
	return false
}

// Subscribe handler, upgrades to a websocket, close to unsubscribe.
func (ps *PubSub) Subscribe(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer c.Close(websocket.StatusInternalError, "unexpected close")

	log.Printf("%s subscribed", r.RemoteAddr)
	defer log.Printf("%s unsubscribed", r.RemoteAddr)

	sub := newSubscriber(32) // TODO: decide on buffer size
	defer sub.Close()
	ps.addSubscriber(sub)

loop:
	for {
		select {
		case data := <-sub.ch:
			out, err := c.Writer(r.Context(), websocket.MessageText)
			if err != nil {
				log.Printf("writer error")
				break loop
			}
			err = json.NewEncoder(out).Encode(map[string]interface{}{
				"data": data,
			})
			err2 := out.Close()
			if err != nil || err2 != nil {
				log.Printf("encoder error")
				break loop
			}
		case <-sub.done:
			log.Printf("subscription done")
			break loop
		}
	}

	c.Close(websocket.StatusNormalClosure, "")
}

// Publish handler, publishes a single message.
func (ps *PubSub) Publish(w http.ResponseWriter, r *http.Request) {
	data := r.FormValue("data")

	ps.mx.RLock()
	defer ps.mx.RUnlock()

	timeout := time.After(time.Minute)
	for _, sub := range ps.subscribers {
		go func(sub *subscriber) {
			select {
			case sub.ch <- data: // Delivered.
			case <-sub.done: // Closed.
			case <-timeout: // They took too long.
				ps.removeSubscriber(sub)
			}
		}(sub)
	}
}

type subscriber struct {
	ch     chan string
	done   chan struct{}
	closer sync.Once
}

func (sub *subscriber) Close() error {
	sub.closer.Do(func() {
		close(sub.done)
	})
	return nil
}

func newSubscriber(buffer int) *subscriber {
	return &subscriber{
		ch:   make(chan string, buffer),
		done: make(chan struct{}),
	}
}
