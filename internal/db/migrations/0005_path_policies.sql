-- +goose Up
create table if not exists path_policies (
  id integer primary key,
  repository_id integer not null,

  pattern text not null,
  kind text not null default 'block',
  reason text,

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),

  unique (repository_id, pattern, kind),
  foreign key (repository_id) references repositories(id) on delete cascade
);

create index if not exists idx_path_policies_repository_id on path_policies(repository_id);

-- +goose Down
drop index if exists idx_path_policies_repository_id;

drop table if exists path_policies;