package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

func Main() { otp.Print(otp.CallGlobal("echo", "hello").(string)) }
