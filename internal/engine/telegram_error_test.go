package engine

import (
	"errors"
	"testing"
)

func TestIsTGMsgIDInvalidMatchesWrappedText(t *testing.T) {
	err := errors.New("get source discussion message: rpcDoRequest: rpc error code 400: MSG_ID_INVALID")
	if !isTGMsgIDInvalid(err) {
		t.Fatalf("expected MSG_ID_INVALID to be detected")
	}
}

func TestIsTGMsgIDInvalidRejectsOtherErrors(t *testing.T) {
	err := errors.New("rpcDoRequest: rpc error code 400: PEER_ID_INVALID")
	if isTGMsgIDInvalid(err) {
		t.Fatalf("unexpected MSG_ID_INVALID match")
	}
}
