package echosup

import (
	"go.muehmer.eu/wintermute/pkg/otp"

	"go.muehmer.eu/wintermute/testdata/otpapp/go/echoserver"
)

type Sup struct{}

func (Sup) Init() []otp.Child {
	return []otp.Child{{ID: "echo", Start: echoserver.Start}}
}
