-- Keep original reference bytes below the documented GPT image 50 MB input
-- limit. Pixel dimensions are metadata, not an unofficial 20 MP provider cap;
-- decoding is admitted separately against the application memory budget.
-- This follows dsapi_003/004 so fresh databases create the gallery first.
DO $$
DECLARE constraint_row RECORD;
BEGIN
    FOR constraint_row IN
        SELECT c.conname
        FROM pg_constraint c
        WHERE c.conrelid = 'image_studio_gallery_assets'::regclass
          AND c.contype = 'c'
          AND (
              (SELECT array_agg(a.attname::text ORDER BY a.attname)
               FROM unnest(c.conkey) AS k(attnum)
               JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)
                 = ARRAY['byte_size']::text[]
              OR
              (SELECT array_agg(a.attname::text ORDER BY a.attname)
               FROM unnest(c.conkey) AS k(attnum)
               JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)
                 = ARRAY['height','width']::text[]
          )
    LOOP
        EXECUTE format('ALTER TABLE image_studio_gallery_assets DROP CONSTRAINT %I', constraint_row.conname);
    END LOOP;
    ALTER TABLE image_studio_gallery_assets
        ADD CONSTRAINT image_studio_gallery_assets_official_bytes_check
            CHECK (byte_size > 0 AND byte_size < 50000000),
        ADD CONSTRAINT image_studio_gallery_assets_positive_dimensions_check
            CHECK (width > 0 AND height > 0);
END $$;
