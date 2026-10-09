UPDATE users SET email = lower(email) WHERE email <> lower(email);

ALTER TABLE users ADD CONSTRAINT users_email_lowercase CHECK (email = lower(email));
