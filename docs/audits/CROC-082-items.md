# CROC-082: item audit

Every item in production (393), read from `GET /items` on 2026-10-11.
Edit the **Proposed** column (and the tables above it) wherever you disagree,
then say it's approved. The migrations are written from this file as it stands.

- 107 items move category, 3 merge, 2 are deleted, 7 are renamed.
- 23 rows have a note.
- **Excluded** categories (House, Ready Meal, Snacks and Sweets) can't be used in recipes: the recipe
  form hides them and the API rejects a recipe containing one. Only items no recipe uses go there.
- **In recipes** counts the recipes using each item, from the cutover export.

## Category totals

| Category | Now | Proposed |
| --- | ---: | ---: |
| Bakery | 1 | 11 |
| Baking (new) | 0 | 20 |
| Chutneys and Spreads (new) | 0 | 12 |
| Condiments | 0 | 15 |
| Cupboard | 151 | 73 |
| Dairy | 24 | 23 |
| Drinks | 23 | 23 |
| Fish | 9 | 9 |
| Frozen (new) | 0 | 5 |
| Fruit | 14 | 12 |
| Herbs and Spices | 61 | 53 |
| House (excluded) | 30 | 27 |
| Meat | 32 | 31 |
| Ready Meal (excluded) | 0 | 2 |
| Sides (new) | 0 | 4 |
| Snacks and Sweets (renamed from Sweets, excluded) | 10 | 4 |
| Veg | 38 | 39 |
| World Foods (new) | 0 | 25 |

## Merges and deletions

A merge points every recipe ingredient, shopping-list row and regular at the kept item, then deletes
the merged one. These run in their own migration, after the category changes.

| Item | In recipes | Action | Note |
| --- | ---: | --- | --- |
| Five-spice mix | 1 | Merge into Chinese 5 spice (6) |  |
| Dill (chopped) | 2 | Merge into Dill (3) |  |
| Pepper (herb) | 8 | Merge into Black pepper (3) | Check: assumed ground black pepper. |
| Steak | 0 | Delete | Duplicate of Steak (Beef). |
| Washing Up Tablets | 0 | Delete | Duplicate of Dish Washer Tablets. |

## Typo fixes

Renames keep the item's id, so recipes, menus and lists that use it follow the new name.

| Now | Rename to |
| --- | --- |
| Dish Washer Tablets | Dishwasher Tablets |
| Guiness | Guinness |
| Icecubes | Ice cubes |
| Lemon Sorbert | Lemon Sorbet |
| Pain au Chocolate | Pain au Chocolat |
| Tumeric | Turmeric |
| Water chestunuts | Water chestnuts |

## Bakery (1)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Lemon (Juice) | 2 | Bakery | **Cupboard** | Bottled lemon juice, assumed. Fruit if you mean fresh. |

## Cupboard (151)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Bagels | 1 | Cupboard | **Bakery** |  |
| Bread | 7 | Cupboard | **Bakery** |  |
| Bread (sourdough) | 4 | Cupboard | **Bakery** |  |
| Burger Buns | 6 | Cupboard | **Bakery** |  |
| Burger Buns (Brioche) | 11 | Cupboard | **Bakery** |  |
| Ciabatta | 3 | Cupboard | **Bakery** |  |
| Crumpets | 1 | Cupboard | **Bakery** |  |
| Flatbread | 1 | Cupboard | **Bakery** |  |
| Naan bread | 2 | Cupboard | **Bakery** |  |
| Pitta bread | 3 | Cupboard | **Bakery** |  |
| Baking Powder | 16 | Cupboard | **Baking** |  |
| Baking Soda | 6 | Cupboard | **Baking** |  |
| Cocoa Powder | 3 | Cupboard | **Baking** |  |
| Coconut (Desiccated) | 1 | Cupboard | **Baking** | Its one recipe is savoury, but it's sold in the baking aisle. |
| Filo Pastry | 2 | Cupboard | **Baking** |  |
| Flour (Bread Wholemeal) | 1 | Cupboard | **Baking** |  |
| Flour (Bread) | 3 | Cupboard | **Baking** |  |
| Flour (Self-Raising) | 11 | Cupboard | **Baking** |  |
| Flour (White) | 32 | Cupboard | **Baking** |  |
| Puff Pastry | 7 | Cupboard | **Baking** |  |
| Shortcrust pastry | 1 | Cupboard | **Baking** |  |
| Sugar (Brown) | 15 | Cupboard | **Baking** |  |
| Sugar (Icing) | 3 | Cupboard | **Baking** |  |
| Sugar (White) | 20 | Cupboard | **Baking** |  |
| Treacle | 1 | Cupboard | **Baking** |  |
| Vanilla Extract | 10 | Cupboard | **Baking** |  |
| Yeast | 4 | Cupboard | **Baking** |  |
| Apricot Jam | 1 | Cupboard | **Chutneys and Spreads** |  |
| Chutney (apple cider) | 3 | Cupboard | **Chutneys and Spreads** | Pending Zoe: chutneys could get their own category later. |
| Chutney (chilli jam) | 2 | Cupboard | **Chutneys and Spreads** | Pending Zoe: chutneys could get their own category later. |
| Chutney (Cranberry) | 0 | Cupboard | **Chutneys and Spreads** | Pending Zoe: chutneys could get their own category later. |
| Chutney (onion) | 2 | Cupboard | **Chutneys and Spreads** | Pending Zoe: chutneys could get their own category later. |
| Mango Chutney | 7 | Cupboard | **Chutneys and Spreads** | Pending Zoe: chutneys could get their own category later. |
| Marmite | 1 | Cupboard | **Chutneys and Spreads** |  |
| Peanut Butter | 4 | Cupboard | **Chutneys and Spreads** |  |
| Strawberry Jam | 2 | Cupboard | **Chutneys and Spreads** |  |
| Bbq Sauce | 5 | Cupboard | **Condiments** |  |
| Chilli Sauce | 8 | Cupboard | **Condiments** |  |
| Garlic Aioli | 0 | Cupboard | **Condiments** |  |
| Hollandaise sauce | 1 | Cupboard | **Condiments** | Unsure: could stay in Cupboard as a cooking sauce. |
| Horseradish sauce | 1 | Cupboard | **Condiments** |  |
| Hot Sauce | 4 | Cupboard | **Condiments** |  |
| Ketchup | 8 | Cupboard | **Condiments** |  |
| Mayonnaise | 23 | Cupboard | **Condiments** |  |
| Mustard (Dijon) | 11 | Cupboard | **Condiments** |  |
| Mustard (Wholegrain) | 3 | Cupboard | **Condiments** |  |
| Mustard (Yellow) | 3 | Cupboard | **Condiments** |  |
| Peri-peri sauce | 1 | Cupboard | **Condiments** |  |
| Salsa (Tomato) | 1 | Cupboard | **Condiments** |  |
| Sriracha | 9 | Cupboard | **Condiments** |  |
| Tabasco | 1 | Cupboard | **Condiments** |  |
| Coffee | 0 | Cupboard | **Drinks** |  |
| Tea | 0 | Cupboard | **Drinks** |  |
| Hash browns | 0 | Cupboard | **Frozen** |  |
| Soup (Tomato) | 0 | Cupboard | **Ready Meal** | Tinned, assumed. |
| Garlic bread | 0 | Cupboard | **Sides** |  |
| Hummus | 0 | Cupboard | **Sides** |  |
| Onion bhajis | 0 | Cupboard | **Sides** |  |
| Poppadoms | 0 | Cupboard | **Sides** |  |
| Crackers | 0 | Cupboard | **Snacks and Sweets** |  |
| Crisps | 0 | Cupboard | **Snacks and Sweets** |  |
| Beans (Edamame) | 1 | Cupboard | **Veg** | Usually bought frozen, like peas. |
| Bao buns | 1 | Cupboard | **World Foods** |  |
| Chinese pancakes | 1 | Cupboard | **World Foods** |  |
| Coconut Milk | 4 | Cupboard | **World Foods** |  |
| Fajita Mix | 1 | Cupboard | **World Foods** |  |
| Gochujang | 1 | Cupboard | **World Foods** |  |
| Harissa paste | 3 | Cupboard | **World Foods** |  |
| Hoisin Sauce | 5 | Cupboard | **World Foods** |  |
| Ketjap Manis | 1 | Cupboard | **World Foods** |  |
| Miso | 2 | Cupboard | **World Foods** |  |
| Oyster sauce | 1 | Cupboard | **World Foods** |  |
| Rogan Josh Curry Paste | 1 | Cupboard | **World Foods** |  |
| Sesame Oil | 6 | Cupboard | **World Foods** |  |
| Soy Sauce | 31 | Cupboard | **World Foods** |  |
| Tacos | 4 | Cupboard | **World Foods** |  |
| Tahini | 1 | Cupboard | **World Foods** |  |
| Tamarind paste | 1 | Cupboard | **World Foods** |  |
| Teriyaki Sauce | 3 | Cupboard | **World Foods** |  |
| Thai Red Curry Paste | 1 | Cupboard | **World Foods** |  |
| Tortillas | 9 | Cupboard | **World Foods** |  |
| Vinegar (Rice Wine) | 4 | Cupboard | **World Foods** |  |
| Water chestunuts | 1 | Cupboard | **World Foods** |  |
| Wonton wrappers | 2 | Cupboard | **World Foods** |  |
| Almonds | 4 | Cupboard | Cupboard |  |
| Almonds (flaked) | 2 | Cupboard | Cupboard |  |
| Almonds (ground) | 2 | Cupboard | Cupboard |  |
| Beans (Black) | 9 | Cupboard | Cupboard |  |
| Beans (Cannellini) | 1 | Cupboard | Cupboard |  |
| Beans (Kidney) | 5 | Cupboard | Cupboard |  |
| Beans (Pinto) | 2 | Cupboard | Cupboard |  |
| Breadcrumbs | 12 | Cupboard | Cupboard |  |
| Butter Beans | 1 | Cupboard | Cupboard |  |
| Cashews | 1 | Cupboard | Cupboard |  |
| Chestnuts | 1 | Cupboard | Cupboard |  |
| Chia seeds | 1 | Cupboard | Cupboard |  |
| Chickpeas | 5 | Cupboard | Cupboard |  |
| Cornflakes | 3 | Cupboard | Cupboard |  |
| Cornflour | 22 | Cupboard | Cupboard |  |
| Curry sauce | 1 | Cupboard | Cupboard |  |
| Dried fruit mix | 1 | Cupboard | Cupboard |  |
| Eggs | 75 | Cupboard | Cupboard |  |
| Flaxseed | 1 | Cupboard | Cupboard |  |
| Garlic Paste | 3 | Cupboard | Cupboard |  |
| Gherkins | 1 | Cupboard | Cupboard |  |
| Ginger (puree) | 3 | Cupboard | Cupboard |  |
| Granola | 0 | Cupboard | Cupboard |  |
| Lasagne sheets | 1 | Cupboard | Cupboard |  |
| Lentils | 3 | Cupboard | Cupboard |  |
| Lentils (Green) | 3 | Cupboard | Cupboard |  |
| Noodles | 3 | Cupboard | Cupboard |  |
| Olive Oil | 98 | Cupboard | Cupboard |  |
| Olives | 1 | Cupboard | Cupboard |  |
| Orzo | 1 | Cupboard | Cupboard |  |
| Panko Breadcrumbs | 13 | Cupboard | Cupboard |  |
| Pasta | 2 | Cupboard | Cupboard |  |
| Peanuts | 2 | Cupboard | Cupboard |  |
| Pesto (green) | 1 | Cupboard | Cupboard |  |
| Pesto (Red) | 1 | Cupboard | Cupboard |  |
| Pickle juice | 1 | Cupboard | Cupboard |  |
| Pickled red onion | 0 | Cupboard | Cupboard |  |
| Pickles | 1 | Cupboard | Cupboard |  |
| Pine Nuts | 2 | Cupboard | Cupboard |  |
| Polenta | 1 | Cupboard | Cupboard |  |
| Porridge oats | 6 | Cupboard | Cupboard |  |
| Protein powder | 3 | Cupboard | Cupboard |  |
| Pumpkin seeds | 1 | Cupboard | Cupboard |  |
| Raisins | 0 | Cupboard | Cupboard |  |
| Ravioli | 1 | Cupboard | Cupboard |  |
| Rice (Basmati) | 12 | Cupboard | Cupboard |  |
| Rice (Brown) | 0 | Cupboard | Cupboard |  |
| Rice (Paella) | 1 | Cupboard | Cupboard |  |
| Rice (Risotto) | 4 | Cupboard | Cupboard |  |
| Spaghetti | 3 | Cupboard | Cupboard |  |
| Stock Cube (Beef) | 1 | Cupboard | Cupboard |  |
| Stock Cube (Chicken) | 15 | Cupboard | Cupboard |  |
| Stock paste (beef) | 2 | Cupboard | Cupboard |  |
| Stock Paste (Chicken) | 3 | Cupboard | Cupboard |  |
| Sweetener (granulated) | 1 | Cupboard | Cupboard |  |
| Tomato (Puree) | 20 | Cupboard | Cupboard |  |
| Tomatoes (Chopped) | 13 | Cupboard | Cupboard |  |
| Tomatoes (Passata) | 8 | Cupboard | Cupboard |  |
| Tortellini | 0 | Cupboard | Cupboard |  |
| Vinegar (Balsamic) | 5 | Cupboard | Cupboard |  |
| Vinegar (Cider) | 5 | Cupboard | Cupboard |  |
| Vinegar (Malt) | 1 | Cupboard | Cupboard |  |
| Vinegar (Red Wine) | 5 | Cupboard | Cupboard |  |
| Vinegar (White) | 5 | Cupboard | Cupboard |  |
| Walnuts | 3 | Cupboard | Cupboard |  |
| Water | 35 | Cupboard | Cupboard | Kept off shopping lists and the filter by CROC-073 / CFE-058. |
| Worcestershire Sauce | 7 | Cupboard | Cupboard |  |

## Dairy (24)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Boscaiola Sauce | 1 | Dairy | **Cupboard** | A creamy pasta sauce, bought in a jar or chilled. Next to Curry sauce. |
| American Cheese | 0 | Dairy | Dairy |  |
| Blue Cheese | 3 | Dairy | Dairy |  |
| Butter | 45 | Dairy | Dairy |  |
| Cheddar | 23 | Dairy | Dairy |  |
| Cream (Double) | 6 | Dairy | Dairy |  |
| Cream (Single) | 1 | Dairy | Dairy |  |
| Cream Cheese | 4 | Dairy | Dairy |  |
| Creme Fraiche | 3 | Dairy | Dairy |  |
| Feta Cheese | 11 | Dairy | Dairy |  |
| Goat's Cheese | 1 | Dairy | Dairy |  |
| Greek Yoghurt | 9 | Dairy | Dairy |  |
| Gruyère | 1 | Dairy | Dairy |  |
| Halloumi | 5 | Dairy | Dairy |  |
| Mascarpone | 0 | Dairy | Dairy |  |
| Milk | 23 | Dairy | Dairy |  |
| Mozzarella | 8 | Dairy | Dairy |  |
| Mozzarella (Grated) | 7 | Dairy | Dairy |  |
| Parmesan | 20 | Dairy | Dairy |  |
| Ricotta | 4 | Dairy | Dairy |  |
| Soured Cream | 5 | Dairy | Dairy |  |
| Stilton | 0 | Dairy | Dairy |  |
| Tofu | 1 | Dairy | Dairy | Not dairy, but chilled. Fine here until a meat-free category earns its place. |
| Yoghurt | 14 | Dairy | Dairy |  |

## Drinks (23)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Icecubes | 2 | Drinks | **Frozen** |  |
| Shaoxing Wine | 2 | Drinks | **World Foods** |  |
| Apple Juice | 0 | Drinks | Drinks |  |
| Beer | 1 | Drinks | Drinks |  |
| Bourbon | 1 | Drinks | Drinks |  |
| Cider | 1 | Drinks | Drinks |  |
| Cola | 1 | Drinks | Drinks |  |
| Elderflower cordial | 0 | Drinks | Drinks |  |
| Gin | 0 | Drinks | Drinks |  |
| Guiness | 2 | Drinks | Drinks |  |
| Lemonade | 0 | Drinks | Drinks |  |
| Lucozade | 0 | Drinks | Drinks |  |
| Orange Juice | 2 | Drinks | Drinks |  |
| Pineapple juice | 1 | Drinks | Drinks |  |
| Prosecco | 0 | Drinks | Drinks |  |
| Soda water | 0 | Drinks | Drinks |  |
| Sparkling water | 1 | Drinks | Drinks |  |
| Squash (Cordial) | 0 | Drinks | Drinks |  |
| Tomato Juice | 1 | Drinks | Drinks |  |
| Tonic Water | 0 | Drinks | Drinks |  |
| Vodka | 1 | Drinks | Drinks |  |
| Wine (Red) | 3 | Drinks | Drinks |  |
| Wine (White) | 1 | Drinks | Drinks |  |

## Fish (9)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Basa Fillets | 2 | Fish | Fish |  |
| Cod fillets | 4 | Fish | Fish |  |
| Crab meat | 1 | Fish | Fish |  |
| King prawns | 1 | Fish | Fish |  |
| Monkfish | 0 | Fish | Fish |  |
| Mussels | 1 | Fish | Fish |  |
| Prawns | 3 | Fish | Fish |  |
| Salmon | 6 | Fish | Fish |  |
| Sardines | 0 | Fish | Fish |  |

## Fruit (14)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Apricot (dried) | 2 | Fruit | **Cupboard** | With Raisins and Dried fruit mix. |
| Sultanas | 1 | Fruit | **Cupboard** | With Raisins and Dried fruit mix. |
| Frozen Berries | 0 | Fruit | **Frozen** |  |
| Apples | 3 | Fruit | Fruit |  |
| Bananas | 4 | Fruit | Fruit |  |
| Blueberries | 0 | Fruit | Fruit |  |
| Lemon | 28 | Fruit | Fruit |  |
| Lime | 18 | Fruit | Fruit |  |
| Mango | 3 | Fruit | Fruit |  |
| Orange | 3 | Fruit | Fruit |  |
| Peach | 1 | Fruit | Fruit |  |
| Pears | 1 | Fruit | Fruit |  |
| Pineapple | 1 | Fruit | Fruit |  |
| Raspberries | 2 | Fruit | Fruit |  |

## Herbs and Spices (61)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Stock Cube (Veg) | 12 | Herbs and Spices | **Cupboard** | With the other stock cubes and pastes. |
| Stock Powder (Chicken) | 1 | Herbs and Spices | **Cupboard** | With the other stock cubes and pastes. |
| Pomegranate seeds | 2 | Herbs and Spices | **Fruit** |  |
| Chipotle Paste | 6 | Herbs and Spices | **World Foods** |  |
| Madras curry paste | 0 | Herbs and Spices | **World Foods** |  |
| Pepper (herb) | 8 | Herbs and Spices | **merge into Black pepper** | Check: assumed ground black pepper. |
| Five-spice mix | 1 | Herbs and Spices | **merge into Chinese 5 spice** |  |
| Dill (chopped) | 2 | Herbs and Spices | **merge into Dill** |  |
| Allspice | 0 | Herbs and Spices | Herbs and Spices |  |
| American burger seasoning | 1 | Herbs and Spices | Herbs and Spices |  |
| Baharat | 1 | Herbs and Spices | Herbs and Spices |  |
| Basil (Dried) | 1 | Herbs and Spices | Herbs and Spices |  |
| Basil (Fresh) | 16 | Herbs and Spices | Herbs and Spices |  |
| Bay leaves | 5 | Herbs and Spices | Herbs and Spices |  |
| Black pepper | 3 | Herbs and Spices | Herbs and Spices |  |
| Cajun seasoning | 3 | Herbs and Spices | Herbs and Spices |  |
| Cardamom pods | 4 | Herbs and Spices | Herbs and Spices |  |
| Cayenne Pepper | 7 | Herbs and Spices | Herbs and Spices |  |
| Celery salt | 1 | Herbs and Spices | Herbs and Spices |  |
| Chilli Flakes | 15 | Herbs and Spices | Herbs and Spices |  |
| Chilli Powder | 14 | Herbs and Spices | Herbs and Spices |  |
| Chinese 5 spice | 6 | Herbs and Spices | Herbs and Spices |  |
| Chives | 3 | Herbs and Spices | Herbs and Spices |  |
| Cinnamon | 12 | Herbs and Spices | Herbs and Spices |  |
| Cinnamon Stick | 2 | Herbs and Spices | Herbs and Spices |  |
| Cloves (Ground) | 1 | Herbs and Spices | Herbs and Spices |  |
| Coriander (Fresh) | 26 | Herbs and Spices | Herbs and Spices |  |
| Coriander (Ground) | 6 | Herbs and Spices | Herbs and Spices |  |
| Coriander Seeds | 1 | Herbs and Spices | Herbs and Spices |  |
| Cumin | 24 | Herbs and Spices | Herbs and Spices |  |
| Cumin (seeds) | 1 | Herbs and Spices | Herbs and Spices |  |
| Curry powder | 4 | Herbs and Spices | Herbs and Spices |  |
| Dill | 3 | Herbs and Spices | Herbs and Spices |  |
| Garam masala | 3 | Herbs and Spices | Herbs and Spices |  |
| Garlic Powder | 24 | Herbs and Spices | Herbs and Spices |  |
| Ginger | 13 | Herbs and Spices | Herbs and Spices | Ground ginger, assumed (Ginger (Root) is in Veg). |
| Italian seasoning | 2 | Herbs and Spices | Herbs and Spices |  |
| Mint | 3 | Herbs and Spices | Herbs and Spices |  |
| Mixed herbs | 3 | Herbs and Spices | Herbs and Spices |  |
| Mixed spice | 2 | Herbs and Spices | Herbs and Spices |  |
| Mustard Powder | 1 | Herbs and Spices | Herbs and Spices |  |
| Nigella Seeds | 1 | Herbs and Spices | Herbs and Spices |  |
| Nutmeg | 3 | Herbs and Spices | Herbs and Spices |  |
| Onion powder | 7 | Herbs and Spices | Herbs and Spices |  |
| Oregano | 21 | Herbs and Spices | Herbs and Spices |  |
| Paprika | 33 | Herbs and Spices | Herbs and Spices |  |
| Paprika (Smoked) | 12 | Herbs and Spices | Herbs and Spices |  |
| Parsley | 22 | Herbs and Spices | Herbs and Spices |  |
| Peppercorns | 1 | Herbs and Spices | Herbs and Spices |  |
| Rosemary | 8 | Herbs and Spices | Herbs and Spices |  |
| Saffron | 1 | Herbs and Spices | Herbs and Spices |  |
| Sage (Dried) | 2 | Herbs and Spices | Herbs and Spices |  |
| Sage leaves | 2 | Herbs and Spices | Herbs and Spices |  |
| Salt | 33 | Herbs and Spices | Herbs and Spices |  |
| Sea salt flakes | 1 | Herbs and Spices | Herbs and Spices |  |
| Sesame Seeds | 20 | Herbs and Spices | Herbs and Spices |  |
| Star anise | 1 | Herbs and Spices | Herbs and Spices |  |
| Thyme (Dried) | 1 | Herbs and Spices | Herbs and Spices |  |
| Thyme (Fresh) | 9 | Herbs and Spices | Herbs and Spices |  |
| Tumeric | 16 | Herbs and Spices | Herbs and Spices |  |
| Wild Garlic | 1 | Herbs and Spices | Herbs and Spices |  |

## House (30)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Pizza | 0 | House | **Ready Meal** |  |
| Tortilla chips | 0 | House | **Snacks and Sweets** |  |
| Washing Up Tablets | 0 | House | **delete** | Duplicate of Dish Washer Tablets. |
| Aluminium Foil | 0 | House | House |  |
| Baking Paper | 0 | House | House |  |
| Bandages | 0 | House | House |  |
| Berocca | 0 | House | House |  |
| Bin Bags | 0 | House | House |  |
| Bleach | 0 | House | House |  |
| Cling Film | 0 | House | House |  |
| Dish Washer Tablets | 0 | House | House |  |
| Fairy Liquid | 0 | House | House |  |
| Food Waste Bags | 0 | House | House |  |
| Hand Soap | 0 | House | House |  |
| Kitchen Roll | 0 | House | House |  |
| Laundry Tablets | 0 | House | House |  |
| Matches | 0 | House | House |  |
| Moisturiser | 0 | House | House |  |
| Mould Spray | 0 | House | House |  |
| Muffin Cases | 0 | House | House |  |
| Razors | 0 | House | House |  |
| Shampoo | 0 | House | House |  |
| Shaving cream | 0 | House | House |  |
| Shower Gel | 0 | House | House |  |
| Sun Cream | 0 | House | House |  |
| Tampons | 0 | House | House |  |
| Toilet Paper | 0 | House | House |  |
| Toothpaste | 0 | House | House |  |
| Tupperware | 0 | House | House |  |
| Weed killer | 0 | House | House |  |

## Meat (32)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Steak | 0 | Meat | **delete** | Duplicate of Steak (Beef). |
| Bacon | 6 | Meat | Meat |  |
| Bacon (Lardons) | 2 | Meat | Meat |  |
| Beef joint | 1 | Meat | Meat |  |
| Beef Mince | 8 | Meat | Meat |  |
| Beef patties | 1 | Meat | Meat |  |
| Beef Ribs | 1 | Meat | Meat |  |
| Braising Steak | 1 | Meat | Meat |  |
| Chicken | 14 | Meat | Meat |  |
| Chicken (Breast) | 17 | Meat | Meat |  |
| Chicken (cooked) | 3 | Meat | Meat |  |
| Chicken (drumstick) | 1 | Meat | Meat |  |
| Chicken (Thighs) | 8 | Meat | Meat |  |
| Chicken (Wings) | 1 | Meat | Meat |  |
| Chorizo | 6 | Meat | Meat |  |
| Cocktail sausages | 1 | Meat | Meat |  |
| Gammon Steaks | 1 | Meat | Meat |  |
| Ham | 2 | Meat | Meat |  |
| Lamb chops | 1 | Meat | Meat |  |
| Lamb Mince | 2 | Meat | Meat |  |
| Pancetta | 1 | Meat | Meat |  |
| Pepperoni | 2 | Meat | Meat |  |
| Pork (Joint) | 1 | Meat | Meat |  |
| Pork (Mince) | 6 | Meat | Meat |  |
| Pork Belly | 1 | Meat | Meat |  |
| Pork Chops | 3 | Meat | Meat |  |
| Pork ribs | 1 | Meat | Meat |  |
| Sausages | 6 | Meat | Meat |  |
| Steak (Beef) | 2 | Meat | Meat |  |
| Turkey (Mince) | 1 | Meat | Meat |  |
| Whole Chicken | 1 | Meat | Meat |  |
| Whole duck | 1 | Meat | Meat |  |

## Sweets (10), renamed to Snacks and Sweets

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Pain au Chocolate | 0 | Sweets | **Bakery** |  |
| Chocolate (Dark) | 1 | Sweets | **Baking** |  |
| Chocolate (Milk) | 1 | Sweets | **Baking** |  |
| Chocolate (White) | 1 | Sweets | **Baking** |  |
| Honey | 31 | Sweets | **Chutneys and Spreads** |  |
| Maple syrup | 1 | Sweets | **Chutneys and Spreads** |  |
| Nutella | 0 | Sweets | **Chutneys and Spreads** |  |
| Ice cream | 0 | Sweets | **Frozen** |  |
| Lemon Sorbert | 0 | Sweets | **Frozen** |  |
| Kit Kats | 0 | Sweets | Snacks and Sweets |  |

## Veg (38)

| Item | In recipes | Now | Proposed | Note |
| --- | ---: | --- | --- | --- |
| Asparagus | 1 | Veg | Veg |  |
| Aubergine | 6 | Veg | Veg |  |
| Avocado | 6 | Veg | Veg |  |
| Baby Potatoes | 2 | Veg | Veg |  |
| Beetroot | 4 | Veg | Veg |  |
| Broccoli | 3 | Veg | Veg |  |
| Broccoli (Tenderstem) | 2 | Veg | Veg |  |
| Butternut Squash | 6 | Veg | Veg |  |
| Carrot | 17 | Veg | Veg |  |
| Cauliflower (Florets) | 2 | Veg | Veg |  |
| Celery | 6 | Veg | Veg |  |
| Chilli (Red) | 24 | Veg | Veg |  |
| Courgette | 6 | Veg | Veg |  |
| Cucumber | 7 | Veg | Veg |  |
| Garlic | 98 | Veg | Veg |  |
| Ginger (Root) | 5 | Veg | Veg |  |
| Green Beans | 4 | Veg | Veg |  |
| Jalapeños | 3 | Veg | Veg |  |
| Kale | 2 | Veg | Veg |  |
| Leeks | 2 | Veg | Veg |  |
| Lemongrass | 1 | Veg | Veg |  |
| Lettuce | 19 | Veg | Veg |  |
| Mushrooms | 13 | Veg | Veg |  |
| Onion | 62 | Veg | Veg |  |
| Onion (Red) | 19 | Veg | Veg |  |
| Peas | 3 | Veg | Veg | Bought frozen or tinned; stays in Veg. |
| Pepper (veg) | 27 | Veg | Veg |  |
| Potatoes | 18 | Veg | Veg |  |
| Rocket | 6 | Veg | Veg |  |
| Salad Leaves | 4 | Veg | Veg |  |
| Spinach | 10 | Veg | Veg |  |
| Spring greens | 3 | Veg | Veg |  |
| Spring Onion | 24 | Veg | Veg |  |
| Sweet Potatoes | 13 | Veg | Veg |  |
| Sweetcorn | 8 | Veg | Veg | Bought frozen or tinned; stays in Veg. |
| Tomatoes | 19 | Veg | Veg |  |
| Tomatoes (Cherry) | 9 | Veg | Veg |  |
| Tomatoes (Plum) | 1 | Veg | Veg |  |
