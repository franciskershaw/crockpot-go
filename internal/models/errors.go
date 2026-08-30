package models

import "errors"

var ErrEmailRegisteredWithPassword = errors.New("email already registered with a password account")

var ErrEmailRegisteredWithGoogle = errors.New("email already registered with a google account")

var ErrEmailUnconfirmed = errors.New("email has an existing unconfirmed registration")

var ErrNoActiveEmailVerificationToken = errors.New("no active email verification token for user")

var ErrNoActivePasswordResetToken = errors.New("no active password reset token")

var ErrUserNotFound = errors.New("user not found")

var ErrRefreshTokenFamilyNotFound = errors.New("refresh token family not found")

var ErrItemCategoryNotFound = errors.New("item category not found")

var ErrItemCategoryInUse = errors.New("item category is in use")

var ErrItemCategoryNameTaken = errors.New("item category name already taken")

var ErrItemCategoryIconTaken = errors.New("item category icon already taken")

var ErrUnitNotFound = errors.New("unit not found")

var ErrUnitInUse = errors.New("unit is in use")

var ErrUnitNameTaken = errors.New("unit name already taken")

var ErrUnitAbbreviationTaken = errors.New("unit abbreviation already taken")

var ErrItemNotFound = errors.New("item not found")

var ErrItemInUse = errors.New("item is in use")

var ErrItemNameTaken = errors.New("item name already taken")

var ErrItemInvalidCategory = errors.New("item category does not exist")

var ErrItemInvalidUnit = errors.New("allowed unit does not exist")
