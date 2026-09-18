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

var ErrRecipeCategoryNotFound = errors.New("recipe category not found")

var ErrRecipeCategoryInUse = errors.New("recipe category is in use")

var ErrRecipeCategoryNameTaken = errors.New("recipe category name already taken")

var ErrRecipeInvalidItem = errors.New("recipe ingredient item does not exist")

var ErrRecipeInvalidUnit = errors.New("recipe ingredient unit does not exist")

var ErrRecipeInvalidCategory = errors.New("recipe category does not exist")

var ErrIngredientUnitNotAllowed = errors.New("unit not allowed for item")

var ErrRecipeDuplicateIngredient = errors.New("recipe has a duplicate ingredient item")

var ErrRecipeNotFound = errors.New("recipe not found")

var ErrRecipeForbidden = errors.New("recipe not owned by caller")

var ErrMenuEntryNotFound = errors.New("menu entry not found")

var ErrShoppingListInvalidItem = errors.New("shopping list item does not exist")

var ErrShoppingListInvalidUnit = errors.New("shopping list unit does not exist")

var ErrShoppingListItemNotFound = errors.New("shopping list item not found")
