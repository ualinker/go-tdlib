package client

import (
	"context"
	"fmt"
	"time"
)

type notSupportedAuthorizationState struct {
	state AuthorizationState
}

func (err *notSupportedAuthorizationState) Error() string {
	return fmt.Sprintf("not supported authorization state: %s", err.state.AuthorizationStateConstructor())
}

func NotSupportedAuthorizationState(state AuthorizationState) error {
	return &notSupportedAuthorizationState{
		state: state,
	}
}

type AuthorizationStateHandler interface {
	Handle(client *Client, state AuthorizationState) error
	Close()
}

func Authorize(client *Client) error {
	var authorizationError error

	for {
		state, err := client.GetAuthorizationState(context.Background())
		if err != nil {
			return err
		}

		switch state.AuthorizationStateConstructor() {
		case ConstructorAuthorizationStateWaitTdlibParameters:
			_, err := client.SetTdlibParameters(context.Background(), client.tdlibParams)
			if err != nil {
				return err
			}
		case ConstructorAuthorizationStateClosed:
			return authorizationError
		case ConstructorAuthorizationStateReady:
			// dirty hack for db flush after authorization
			time.Sleep(1 * time.Second)
			return nil
		case ConstructorAuthorizationStateWaitPhoneNumber:
			return nil
		case ConstructorAuthorizationStateWaitCode:
			return nil
		default:
			return NotSupportedAuthorizationState(state)
		}
	}
}

type clientAuthorizer struct {
	TdlibParameters *SetTdlibParametersRequest
	PhoneNumber     chan string
	Code            chan string
	State           chan AuthorizationState
	Password        chan string
}

func ClientAuthorizer(tdlibParameters *SetTdlibParametersRequest) *clientAuthorizer {
	return &clientAuthorizer{
		TdlibParameters: tdlibParameters,
		PhoneNumber:     make(chan string),
		Code:            make(chan string),
		State:           make(chan AuthorizationState),
		Password:        make(chan string),
	}
}

func (stateHandler *clientAuthorizer) Handle(client *Client, state AuthorizationState) error {
	stateHandler.State <- state

	switch state.AuthorizationStateConstructor() {
	case ConstructorAuthorizationStateWaitTdlibParameters:
		_, err := client.SetTdlibParameters(context.Background(), stateHandler.TdlibParameters)
		return err

	case ConstructorAuthorizationStateWaitPhoneNumber:
		_, err := client.SetAuthenticationPhoneNumber(context.Background(), &SetAuthenticationPhoneNumberRequest{
			PhoneNumber: <-stateHandler.PhoneNumber,
			Settings: &PhoneNumberAuthenticationSettings{
				AllowFlashCall:       false,
				IsCurrentPhoneNumber: false,
				AllowSmsRetrieverApi: false,
			},
		})
		return err

	case ConstructorAuthorizationStateWaitEmailAddress:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitEmailCode:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitCode:
		_, err := client.CheckAuthenticationCode(context.Background(), &CheckAuthenticationCodeRequest{
			Code: <-stateHandler.Code,
		})
		return err

	case ConstructorAuthorizationStateWaitOtherDeviceConfirmation:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitRegistration:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitPassword:
		_, err := client.CheckAuthenticationPassword(context.Background(), &CheckAuthenticationPasswordRequest{
			Password: <-stateHandler.Password,
		})
		return err

	case ConstructorAuthorizationStateReady:
		return nil

	case ConstructorAuthorizationStateLoggingOut:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateClosing:
		return nil

	case ConstructorAuthorizationStateClosed:
		return nil
	}

	return NotSupportedAuthorizationState(state)
}

func (stateHandler *clientAuthorizer) Close() {
	close(stateHandler.PhoneNumber)
	close(stateHandler.Code)
	close(stateHandler.State)
	close(stateHandler.Password)
}

func CliInteractor(clientAuthorizer *clientAuthorizer) {
	for {
		select {
		case state, ok := <-clientAuthorizer.State:
			if !ok {
				return
			}

			switch state.AuthorizationStateConstructor() {
			case ConstructorAuthorizationStateWaitPhoneNumber:
				fmt.Println("Enter phone number: ")
				var phoneNumber string
				fmt.Scanln(&phoneNumber)

				clientAuthorizer.PhoneNumber <- phoneNumber

			case ConstructorAuthorizationStateWaitCode:
				var code string

				fmt.Println("Enter code: ")
				fmt.Scanln(&code)

				clientAuthorizer.Code <- code

			case ConstructorAuthorizationStateWaitPassword:
				fmt.Println("Enter password: ")
				var password string
				fmt.Scanln(&password)

				clientAuthorizer.Password <- password

			case ConstructorAuthorizationStateReady:
				return
			}
		}
	}
}

type botAuthorizer struct {
	tdlibParameters *SetTdlibParametersRequest
	token           string
}

func BotAuthorizer(tdlibParameters *SetTdlibParametersRequest, token string) *botAuthorizer {
	return &botAuthorizer{
		tdlibParameters: tdlibParameters,
		token:           token,
	}
}

func (stateHandler *botAuthorizer) Handle(client *Client, state AuthorizationState) error {
	switch state.AuthorizationStateConstructor() {
	case ConstructorAuthorizationStateWaitTdlibParameters:
		_, err := client.SetTdlibParameters(context.Background(), stateHandler.tdlibParameters)
		return err

	case ConstructorAuthorizationStateWaitPhoneNumber:
		_, err := client.CheckAuthenticationBotToken(context.Background(), &CheckAuthenticationBotTokenRequest{
			Token: stateHandler.token,
		})
		return err

	case ConstructorAuthorizationStateWaitEmailAddress:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitEmailCode:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitCode:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitOtherDeviceConfirmation:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitRegistration:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitPassword:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateReady:
		return nil

	case ConstructorAuthorizationStateLoggingOut:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateClosing:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateClosed:
		return NotSupportedAuthorizationState(state)
	}

	return NotSupportedAuthorizationState(state)
}

func (stateHandler *botAuthorizer) Close() {}

type qrAuthorizer struct {
	TdlibParameters *SetTdlibParametersRequest
	Password        chan string
	lastLink        string
	LinkHandler     func(link string) error
}

func QrAuthorizer(tdlibParameters *SetTdlibParametersRequest, linkHandler func(link string) error) *qrAuthorizer {
	stateHandler := &qrAuthorizer{
		TdlibParameters: tdlibParameters,
		Password:        make(chan string),
		LinkHandler:     linkHandler,
	}

	return stateHandler
}

func (stateHandler *qrAuthorizer) Handle(client *Client, state AuthorizationState) error {
	switch state.AuthorizationStateConstructor() {
	case ConstructorAuthorizationStateWaitTdlibParameters:
		_, err := client.SetTdlibParameters(context.Background(), stateHandler.TdlibParameters)
		return err

	case ConstructorAuthorizationStateWaitPhoneNumber:
		_, err := client.RequestQrCodeAuthentication(context.Background(), &RequestQrCodeAuthenticationRequest{})
		return err

	case ConstructorAuthorizationStateWaitOtherDeviceConfirmation:
		link := state.(*AuthorizationStateWaitOtherDeviceConfirmation).Link

		if link == stateHandler.lastLink {
			return nil
		}

		err := stateHandler.LinkHandler(link)
		if err != nil {
			return err
		}

		stateHandler.lastLink = link

		return nil

	case ConstructorAuthorizationStateWaitCode:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateWaitPassword:
		_, err := client.CheckAuthenticationPassword(context.Background(), &CheckAuthenticationPasswordRequest{
			Password: <-stateHandler.Password,
		})
		return err

	case ConstructorAuthorizationStateReady:
		return nil

	case ConstructorAuthorizationStateLoggingOut:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateClosing:
		return NotSupportedAuthorizationState(state)

	case ConstructorAuthorizationStateClosed:
		return NotSupportedAuthorizationState(state)
	}

	return NotSupportedAuthorizationState(state)
}

func (stateHandler *qrAuthorizer) Close() {
	close(stateHandler.Password)
}
