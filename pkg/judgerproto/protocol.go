package judgerproto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lcpu-club/lfs-auto-grader/pkg/aoiclient"
)

type Action string

const (
	ActionGreet    Action = "0"
	ActionNoop     Action = "n"
	ActionError    Action = "e"
	ActionLog      Action = "l"
	ActionComplete Action = "c"
	ActionQuit     Action = "q"
	ActionPatch    Action = "p"
	ActionDetail   Action = "d"
)

type Message struct {
	Time   time.Time       `json:"t"`
	Action Action          `json:"a"`
	Body   json.RawMessage `json:"b,omitempty"`
}

type ErrorBody string
type LogBody string

type PatchBody aoiclient.SolutionInfo
type DetailBody aoiclient.SolutionDetails

func newMessage(action Action, body interface{}) *Message {
	var raw json.RawMessage
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	return &Message{
		Time:   time.Now(),
		Action: action,
		Body:   raw,
	}
}

func NewGreetMessage() *Message {
	return newMessage(ActionGreet, nil)
}

func NewNoopMessage() *Message {
	return newMessage(ActionNoop, nil)
}

func NewErrorMessage(err error) *Message {
	return newMessage(ActionError, ErrorBody(err.Error()))
}

func NewLogMessage(log string) *Message {
	return newMessage(ActionLog, LogBody(log))
}

func NewCompleteMessage() *Message {
	return newMessage(ActionComplete, nil)
}

func NewQuitMessage() *Message {
	return newMessage(ActionQuit, nil)
}

func NewPatchMessage(details *PatchBody) *Message {
	return newMessage(ActionPatch, PatchBody(*details))
}

func NewDetailMessage(details *DetailBody) *Message {
	return newMessage(ActionDetail, DetailBody(*details))
}

func (m *Message) String() string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{\"t\":\"" + time.Now().String() + "\",\"a\":\"e\",\"b\":\"Failed to marshal message\"}"
	}
	return string(b)
}

func (m *Message) Print() {
	fmt.Println(m.String())
}

func MessageFromString(s string) (*Message, error) {
	var m Message
	err := json.Unmarshal([]byte(s), &m)
	return &m, err
}
