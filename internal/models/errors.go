package models

import "errors"

var ErrEmailRegisteredWithPassword = errors.New("email already registered with a password account")
