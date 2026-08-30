ALTER TABLE recipe_categories_recipes
    DROP CONSTRAINT recipe_categories_recipes_category_id_fkey,
    ADD CONSTRAINT recipe_categories_recipes_category_id_fkey
        FOREIGN KEY (category_id) REFERENCES recipe_categories (id) ON DELETE RESTRICT;
