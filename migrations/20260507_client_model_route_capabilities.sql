ALTER TABLE client_model_routes
  ADD COLUMN aspect_ratios JSON NULL AFTER sort_order,
  ADD COLUMN durations JSON NULL AFTER aspect_ratios,
  ADD COLUMN resolutions JSON NULL AFTER durations,
  ADD COLUMN max_images BIGINT NOT NULL DEFAULT 0 AFTER resolutions;
