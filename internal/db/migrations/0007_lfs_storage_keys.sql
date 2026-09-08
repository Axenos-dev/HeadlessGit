-- +goose Up
alter table lfs_objects add column storage_key text not null default '';

update lfs_objects
set storage_key = printf(
  '%d/%s/%s/%s',
  repository_id,
  substr(object_id, 1, 2),
  substr(object_id, 3, 2),
  object_id
);

-- +goose Down
alter table lfs_objects drop column storage_key;
