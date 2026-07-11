package echoapp

import (
	"go.muehmer.eu/wintermute/pkg/otp"

	"go.muehmer.eu/wintermute/testdata/otpapp/go/echosup"
)

type App struct{}

func (App) Start() otp.Pid { return otp.StartSupervisor(echosup.Sup{}) }
func (App) Stop()          {}
