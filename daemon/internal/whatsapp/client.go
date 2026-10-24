package whatsapp

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// pairTimeout bounds the PairPhone network call.
const pairTimeout = 30 * time.Second

// Client is the whatsmeow adapter: the only place whatsmeow types appear.
// Tests never use it: it would reach the WhatsApp servers.
type Client struct {
	cli    *whatsmeow.Client
	events chan Event
	mu     sync.Mutex // one Connect at a time
}

// NewClient loads the device from the device store (a new unpaired device if there is none).
func NewClient(container *sqlstore.Container) (*Client, error) {
	dev, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, err
	}
	c := &Client{cli: whatsmeow.NewClient(dev, waLog.Noop), events: make(chan Event, 64)}
	c.cli.AddEventHandler(c.handle)
	return c, nil
}

func (c *Client) Linked() bool         { return c.cli.Store.ID != nil }
func (c *Client) Events() <-chan Event { return c.events }
func (c *Client) Disconnect()          { c.cli.Disconnect() }

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cli.Disconnect() // a retry starts from a closed socket
	if c.Linked() {
		return c.cli.Connect()
	}
	qrs, err := c.cli.GetQRChannel(context.Background())
	if err != nil {
		return err
	}
	if err := c.cli.Connect(); err != nil {
		return err
	}
	go c.forwardQRs(qrs)
	return nil
}

func (c *Client) forwardQRs(qrs <-chan whatsmeow.QRChannelItem) {
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			c.events <- QRCode{Code: item.Code}
		case whatsmeow.QRChannelSuccess.Event:
			// events.PairSuccess reports success.
		default:
			c.events <- LinkFailed{Reason: qrFailureReason(item)}
		}
	}
}

func qrFailureReason(item whatsmeow.QRChannelItem) string {
	switch item {
	case whatsmeow.QRChannelTimeout:
		return ReasonTimeout
	case whatsmeow.QRChannelClientOutdated:
		return ReasonClientOutdated
	case whatsmeow.QRChannelScannedWithoutMultidevice:
		return ReasonScannedWithoutMultidevice
	}
	return ReasonUnexpected
}

func (c *Client) PairCode(phone string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	code, err := c.cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if errors.Is(err, whatsmeow.ErrPhoneNumberTooShort) || errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational) {
		return "", ErrInvalidPhone
	}
	return code, err
}

func (c *Client) handle(evt any) {
	switch evt.(type) {
	case *events.PairSuccess:
		c.events <- PairSuccess{}
	case *events.PairError:
		c.events <- LinkFailed{Reason: ReasonPairError}
	case *events.ClientOutdated:
		c.events <- LinkFailed{Reason: ReasonClientOutdated}
	case *events.LoggedOut:
		c.events <- LoggedOut{}
	}
}
