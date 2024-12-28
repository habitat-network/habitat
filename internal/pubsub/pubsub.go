package pubsub

import (
	"reflect"

	"github.com/rs/zerolog/log"
)

type Event interface {
}

type Publisher[E Event] interface {
	PublishEvent(E) error
	GetChan() <-chan E
	// AddSubscriber(Subscriber[E])
}

type Subscriber[E Event] interface {
	Name() string
	ConsumeEvent(E) error
}

type Channel[E Event] interface {
	Listen() error
}

// SimplePublisher is an extremely simple implementation of a Publisher that just
// loops through each subscriber and calls ConsumeEvent on it. It is not thread safe,
// and does not ensure the ordering of events in any way. In the future, a more mature
// solution that guarantees these properties is likely needed.
// TODO: Implement a channel based publisher.
// TODO: Implement a topic based publisher.
type SimplePublisher[E Event] struct {
	//subscribers []Subscriber[E]
	channel chan E
}

func newSimplePublisher[E Event]() *SimplePublisher[E] {
	return &SimplePublisher[E]{
		channel: make(chan E),
	}
}

func (p *SimplePublisher[E]) PublishEvent(e E) error {
	p.channel <- e

	return nil
}

func (p *SimplePublisher[E]) GetChan() <-chan E {
	return p.channel
}

type SimpleChannel[E Event] struct {
	subscribers []Subscriber[E]
	publishers  []Publisher[E]
}

func newSimpleChannel[E Event]() *SimpleChannel[E] {
	return &SimpleChannel[E]{
		subscribers: make([]Subscriber[E], 0),
		publishers:  make([]Publisher[E], 0),
	}
}

func (c *SimpleChannel[E]) Listen() error {
	chans := make([]<-chan E, len(c.publishers))
	for i, p := range c.publishers {
		chans[i] = p.GetChan()
	}

	cases := make([]reflect.SelectCase, len(chans))
	for i, ch := range chans {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}

	for {

		_, value, ok := reflect.Select(cases)
		if !ok {
			// TODO figure out what to do in this case
			continue
		}

		for _, sub := range c.subscribers {
			go func(s Subscriber[E], e E) {
				err := s.ConsumeEvent(e)
				if err != nil {
					log.Error().Err(err).Msgf("Subscriber %s had an error while consuming event: %s", s.Name(), err.Error())
				}
			}(sub, value.Interface().(E))
		}

	}
}

func NewSimplePublisher[E Event]() *SimplePublisher[E] {
	return newSimplePublisher[E]()
}

func NewSimpleChannel[E Event](publishers []Publisher[E], subscribers []Subscriber[E]) *SimpleChannel[E] {
	channel := newSimpleChannel[E]()
	channel.publishers = publishers
	channel.subscribers = subscribers
	return channel
}
