CREATE TABLE IF NOT EXISTS building_cache (
  city          text        NOT NULL,
  address_norm  text        NOT NULL,
  lat           double precision NOT NULL,
  lon           double precision NOT NULL,
  floors_total  integer,
  year_built    integer,
  house_material text,
  source        text,                 -- 'osm_overpass' | 'mos_dataset'
  raw_tags      jsonb,                -- что вернул OSM
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (city, address_norm)
);

CREATE INDEX IF NOT EXISTS building_cache_created_at_idx ON building_cache (created_at DESC);
