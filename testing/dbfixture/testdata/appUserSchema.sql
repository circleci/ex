CREATE TABLE public.test_table(
    id   text NOT NULL,
    name text NOT NULL
);
do $$ begin
    IF NOT EXISTS (SELECT * FROM pg_user WHERE usename = 'test_role_1') THEN
        CREATE ROLE test_role_1 LOGIN PASSWORD 'teehee';
    END IF;
end $$;
GRANT USAGE, SELECT on all sequences in schema public to test_role_1;
GRANT SELECT on all tables in schema public to test_role_1;
