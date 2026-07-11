package echoclient

import "go.muehmer.eu/wintermute/pkg/otp"

func Main() { otp.Print(otp.Call("echo", "hello").(string)) }
