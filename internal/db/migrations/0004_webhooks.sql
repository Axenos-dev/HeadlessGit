-- +goose Up
create table if not exists webhooks (
  id integer primary key,
  repository_id integer not null,

  secret text not null,
  url text not null,
 
  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),
  updated_at_unix_ms integer,

  foreign key (repository_id) references repositories(id) on delete cascade
);

create index if not exists idx_webhooks_repository_id on webhooks(repository_id);

-- +goose Down
drop index if exists idx_webhooks_repository_id;

drop table if exists webhooks;
