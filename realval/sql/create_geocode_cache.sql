CREATE TABLE IF NOT EXISTS geocode_cache (
  city         text        NOT NULL,
  address_norm text        NOT NULL,
  lat          double precision NOT NULL,
  lon          double precision NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (city, address_norm)
);

-- полезные индексы
CREATE INDEX IF NOT EXISTS geocode_cache_created_at_idx ON geocode_cache (created_at DESC);
