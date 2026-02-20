package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/initialize"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

func main() {
	if err := initialize.InitConfig(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "init config failed:", err)
		os.Exit(1)
	}

	phone := flag.String("phone", "", "Telegram phone number, e.g. +86138xxxxxxx")
	flag.Parse()

	if *phone == "" {
		_, _ = fmt.Fprintln(os.Stderr, "missing -phone, example: go run ./cmd/auth_tool -phone +86138xxxxxxx")
		os.Exit(2)
	}

	ctx := context.Background()

	client, err := engine.NewTGClient(ctx, *phone)
	if err != nil {
		panic(err)
	}

	if err := client.Client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(terminalAuth{phone: client.Phone}, auth.SendCodeOptions{})
		if err := flow.Run(ctx, client.Client.Auth()); err != nil {
			return err
		}

		fmt.Printf("登录成功，Session 已保存：%s\n", client.SessionPath)
		return nil
	}); err != nil {
		panic(err)
	}
}

type terminalAuth struct {
	phone string
}

func (a terminalAuth) Phone(ctx context.Context) (string, error) {
	return a.phone, nil
}

func (a terminalAuth) Password(ctx context.Context) (string, error) {
	_, _ = fmt.Fprint(os.Stdout, "Enter 2FA password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return "", errors.New("empty password")
	}
	return p, nil
}

func (a terminalAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	return errors.New("sign up is not supported by auth_tool")
}

func (a terminalAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up is not supported by auth_tool")
}

func (a terminalAuth) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	_, _ = fmt.Fprint(os.Stdout, "Enter code: ")
	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("empty code")
	}
	return code, nil
}
