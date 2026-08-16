package models

import "errors"

var ErrEmailRegisteredWithPassword = errors.New("email already registered with a password account")

var ErrNoActiveEmailVerificationToken = errors.New("no active email verification token for user")
