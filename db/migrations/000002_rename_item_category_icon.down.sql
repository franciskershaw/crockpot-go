ALTER TABLE item_categories RENAME CONSTRAINT item_categories_icon_key TO item_categories_fa_icon_key;

ALTER TABLE item_categories RENAME COLUMN icon TO fa_icon;
