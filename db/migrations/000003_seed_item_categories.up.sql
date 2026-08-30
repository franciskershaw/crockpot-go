INSERT INTO item_categories (name, icon) VALUES
    ('Cupboard', 'Package'),
    ('Herbs and Spices', 'Zap'),
    ('Drinks', 'Wine'),
    ('Veg', 'Carrot'),
    ('Condiments', 'BottleWine'),
    ('House', 'House'),
    ('Sweets', 'Cookie'),
    ('Bakery', 'Croissant'),
    ('Meat', 'Beef'),
    ('Dairy', 'Milk'),
    ('Fruit', 'Apple'),
    ('Fish', 'Fish'),
    ('Ready Meal', 'Microwave')
ON CONFLICT (name) DO NOTHING;
