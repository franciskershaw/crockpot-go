CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE
    IF NOT EXISTS users (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        google_id TEXT UNIQUE,
        password_hash TEXT,
        email TEXT UNIQUE NOT NULL,
        name TEXT,
        image TEXT,
        role TEXT NOT NULL DEFAULT 'FREE' CHECK (role IN ('FREE', 'PREMIUM', 'PRO', 'ADMIN')),
        email_verified_at TIMESTAMPTZ,
        last_login_at TIMESTAMPTZ,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        CHECK (
            (google_id IS NOT NULL) != (password_hash IS NOT NULL)
        )
    );

CREATE TABLE
    IF NOT EXISTS refresh_tokens (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        token_hash TEXT NOT NULL,
        previous_token_hash TEXT,
        previous_token_rotated_at TIMESTAMPTZ,
        expires_at TIMESTAMPTZ NOT NULL,
        revoked_at TIMESTAMPTZ,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens (user_id);

CREATE TABLE
    IF NOT EXISTS email_verification_tokens (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        token_hash TEXT NOT NULL,
        expires_at TIMESTAMPTZ NOT NULL,
        used_at TIMESTAMPTZ,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_email_verification_tokens_active_user ON email_verification_tokens (user_id)
WHERE
    used_at IS NULL;

CREATE TABLE
    IF NOT EXISTS password_reset_tokens (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        token_hash TEXT NOT NULL,
        expires_at TIMESTAMPTZ NOT NULL,
        used_at TIMESTAMPTZ,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_password_reset_tokens_active_user ON password_reset_tokens (user_id)
WHERE
    used_at IS NULL;

CREATE TABLE
    IF NOT EXISTS item_categories (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        name TEXT UNIQUE NOT NULL,
        fa_icon TEXT UNIQUE NOT NULL,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS units (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        name TEXT UNIQUE NOT NULL,
        abbreviation TEXT UNIQUE NOT NULL,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS items (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        category_id UUID NOT NULL REFERENCES item_categories (id) ON DELETE RESTRICT,
        name TEXT UNIQUE NOT NULL,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS item_allowed_units (
        item_id UUID NOT NULL REFERENCES items (id) ON DELETE CASCADE,
        unit_id UUID NOT NULL REFERENCES units (id) ON DELETE CASCADE,
        PRIMARY KEY (item_id, unit_id)
    );

CREATE TABLE
    IF NOT EXISTS recipe_categories (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        name TEXT UNIQUE NOT NULL,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS recipes (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        name TEXT NOT NULL,
        description TEXT,
        time_in_minutes INT NOT NULL,
        image_url TEXT,
        image_filename TEXT,
        instructions TEXT[] NOT NULL,
        notes TEXT[],
        approved BOOLEAN NOT NULL DEFAULT false,
        serves INT NOT NULL,
        created_by_id UUID REFERENCES users (id) ON DELETE SET NULL,
        created_by_name TEXT,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS recipe_ingredients (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        recipe_id UUID NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
        item_id UUID NOT NULL REFERENCES items (id) ON DELETE RESTRICT,
        unit_id UUID REFERENCES units (id) ON DELETE RESTRICT,
        quantity NUMERIC(10, 2) NOT NULL,
        UNIQUE (recipe_id, item_id)
    );

CREATE TABLE
    IF NOT EXISTS recipe_categories_recipes (
        recipe_id UUID NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
        category_id UUID NOT NULL REFERENCES recipe_categories (id) ON DELETE CASCADE,
        PRIMARY KEY (recipe_id, category_id)
    );

CREATE INDEX IF NOT EXISTS idx_recipe_categories_recipes_category_id ON recipe_categories_recipes (category_id);

CREATE TABLE
    IF NOT EXISTS recipe_favourites (
        user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        recipe_id UUID NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
        PRIMARY KEY (user_id, recipe_id)
    );

CREATE TABLE
    IF NOT EXISTS recipe_menus (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        user_id UUID UNIQUE NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS recipe_menu_entries (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        recipe_menu_id UUID NOT NULL REFERENCES recipe_menus (id) ON DELETE CASCADE,
        recipe_id UUID NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
        serves INT NOT NULL
    );

CREATE INDEX IF NOT EXISTS idx_recipe_menu_entries_recipe_menu_id ON recipe_menu_entries (recipe_menu_id);

CREATE TABLE
    IF NOT EXISTS menu_history_entries (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        recipe_menu_id UUID NOT NULL REFERENCES recipe_menus (id) ON DELETE CASCADE,
        recipe_id UUID NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
        times_added_to_menu INT NOT NULL DEFAULT 1,
        first_added_to_menu TIMESTAMPTZ NOT NULL,
        last_added_to_menu TIMESTAMPTZ NOT NULL,
        last_removed_from_menu TIMESTAMPTZ NOT NULL
    );

CREATE INDEX IF NOT EXISTS idx_menu_history_entries_recipe_menu_id ON menu_history_entries (recipe_menu_id);

CREATE TABLE
    IF NOT EXISTS shopping_lists (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        user_id UUID UNIQUE NOT NULL REFERENCES users (id) ON DELETE CASCADE,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

CREATE TABLE
    IF NOT EXISTS shopping_list_items (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
        shopping_list_id UUID NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
        item_id UUID NOT NULL REFERENCES items (id) ON DELETE RESTRICT,
        unit_id UUID REFERENCES units (id) ON DELETE RESTRICT,
        quantity NUMERIC(10, 2) NOT NULL,
        obtained BOOLEAN NOT NULL DEFAULT false,
        is_manual BOOLEAN NOT NULL DEFAULT false
    );

CREATE INDEX IF NOT EXISTS idx_shopping_list_items_shopping_list_id ON shopping_list_items (shopping_list_id);
