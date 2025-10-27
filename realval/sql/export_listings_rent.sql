SELECT
  deal_type, city, district,
  price_rub, price_period, price_per_m2,
  rooms, area_total, area_living, area_kitchen,
  floor, floors_total, year_built, house_material, condition,
  lat, lon, dist_to_metro_m, dist_to_center_km, density_500m,
  metro_station, metro_walk_min, first_seen, last_seen
FROM listing_master
WHERE is_active
  AND deal_type = 'rent_long'
