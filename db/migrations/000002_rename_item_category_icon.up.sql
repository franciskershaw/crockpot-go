ALTER TABLE item_categories RENAME COLUMN fa_icon TO icon;

ALTER TABLE item_categories RENAME CONSTRAINT item_categories_fa_icon_key TO item_categories_icon_key;
