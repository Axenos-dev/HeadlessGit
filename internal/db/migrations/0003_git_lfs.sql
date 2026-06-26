-- +goose Up
create table if not exists lfs_objects (
  id integer primary key,
  user_id integer not null,
  repository_id integer not null,

  object_id text not null,
  size_bytes integer not null,
  verified boolean not null default 0,

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),

  unique(object_id, repository_id),
  foreign key (repository_id) references repositories(id) on delete cascade
);

create index if not exists idx_lfs_objects_user_id on lfs_objects(user_id);

-- +goose Down
drop index if exists idx_lfs_objects_user_id;
drop table if exists lfs_objects;
