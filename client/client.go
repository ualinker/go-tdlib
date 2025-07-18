package client

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ualinker/go-tdlib/client/ratelimiter"
	"github.com/ualinker/go-tdlib/client/ratelimiter/strategy"
)

type Client struct {
	jsonClient      *JsonClient
	extraGenerator  ExtraGenerator
	responses       chan *Response
	resultHandler   ResultHandler
	catchersStore   *sync.Map
	fallbackTimeout time.Duration
	isClosed        bool
	tdlibParams     *SetTdlibParametersRequest
	rateLimiter     ratelimiter.RateLimiter
}

type Option func(*Client)

func WithExtraGenerator(extraGenerator ExtraGenerator) Option {
	return func(client *Client) {
		client.extraGenerator = extraGenerator
	}
}

func WithFallbackTimeout(timeout time.Duration) Option {
	return func(client *Client) {
		client.fallbackTimeout = timeout
	}
}

func WithProxy(req *AddProxyRequest) Option {
	return func(client *Client) {
		client.AddProxy(context.Background(), req)
	}
}

func WithResultHandler(resultHandler ResultHandler) Option {
	return func(client *Client) {
		client.resultHandler = resultHandler
	}
}

type ResultHandler interface {
	OnResult(result Type)
}

type CallbackResultHandler struct {
	callback func(result Type)
}

func (handler *CallbackResultHandler) OnResult(result Type) {
	handler.callback(result)
}

func NewCallbackResultHandler(callback func(result Type)) *CallbackResultHandler {
	return &CallbackResultHandler{
		callback: callback,
	}
}

func NewClient(params *SetTdlibParametersRequest, options ...Option) (*Client, error) {
	client := &Client{
		jsonClient:    NewJsonClient(),
		responses:     make(chan *Response, 1000),
		catchersStore: &sync.Map{},
		isClosed:      false,
		tdlibParams:   params,
		rateLimiter:   ratelimiter.NewRateLimiter(strategy.WindUp()),
	}

	SetLogMessageCallback(2, client.rateLimiter.TDLibCallback)
	client.extraGenerator = UuidV4Generator()
	client.resultHandler = NewCallbackResultHandler(func(result Type) {})
	client.fallbackTimeout = 60 * time.Second

	tdlibInstance.addClient(client)
	go client.receiver()

	for _, option := range options {
		go option(client)
	}

	return client, nil
}

func (client *Client) receiver() {
	for response := range client.responses {
		if response.MetaExtra != "" {
			value, ok := client.catchersStore.Load(response.MetaExtra)
			if ok {
				value.(chan *Response) <- response
			}
		}

		typ, err := UnmarshalType(response.Data)
		if err != nil {
			continue
		}

		client.resultHandler.OnResult(typ)

		if typ.GetConstructor() == ConstructorUpdateAuthorizationState &&
			typ.(*UpdateAuthorizationState).AuthorizationState.AuthorizationStateConstructor() == ConstructorAuthorizationStateClosed {
			client.isClosed = true
			close(client.responses)
		}
	}
}

func (client *Client) Send(ctx context.Context, req Request) (*Response, error) {
	req.SetExtra(client.extraGenerator())
	req.SetType(req.GetFunctionName())

	catcher := make(chan *Response)

	client.catchersStore.Store(req.GetExtra(), catcher)

	defer func() {
		client.catchersStore.Delete(req.GetExtra())
		close(catcher)
	}()

	client.rateLimiter.Wait()
	logrus.Infof("go-tdlib: request: %s", req.GetType())
	err := client.jsonClient.Send(req)
	if err != nil {
		return nil, err
	}

	fallbackCtx, cancel := context.WithTimeout(context.Background(), client.fallbackTimeout)
	defer cancel()

	select {
	case response := <-catcher:
		return response, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-fallbackCtx.Done():
		return nil, fallbackCtx.Err()
	}
}

func (client *Client) Execute(req Request) (*Response, error) {
	req.SetExtra(client.extraGenerator())
	req.SetType(req.GetFunctionName())

	return client.jsonClient.Execute(req)
}

func (client *Client) SetResultHandler(resultHandler ResultHandler) {
	client.resultHandler = resultHandler
}

func (client *Client) DropResultHandler() {
	client.resultHandler = NewCallbackResultHandler(func(result Type) {})
}

type RateLimiter interface {
	CurrentRate() float64
}

func (client *Client) RateLimiter() RateLimiter {
	return client.rateLimiter
}
