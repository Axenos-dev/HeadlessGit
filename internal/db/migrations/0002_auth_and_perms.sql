-- +goose Up
create table if not exists ssh_keys (
  id integer primary key,
  user_id integer not null,

  title text not null, -- just human readble name for a key
  public_key text not null,
  fingerprint text not null unique, -- SHA256 fingerprint

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),
  last_used_at_unix_ms integer,

  foreign key (user_id) references users(id) on delete cascade
);

create table if not exists tokens (
  id integer primary key,
  user_id integer not null,

  title text not null, -- just human readble name for a key
  token_hash text not null unique, -- sha256 hashed token

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),
  last_used_at_unix_ms integer,
  expires_at_unix_ms integer,

  foreign key (user_id) references users(id) on delete cascade
);

create table if not exists permissions (
  id integer primary key,
  user_id integer not null,
  repository_id integer not null,

  user_role text not null check (user_role in ('read', 'write', 'admin')),

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),
  updated_at_unix_ms integer,

  unique(user_id, repository_id),
  foreign key (user_id) references users(id) on delete cascade,
  foreign key (repository_id) references repositories(id) on delete cascade
);

create index if not exists idx_ssh_keys_user_id on ssh_keys(user_id);
create index if not exists idx_tokens_user_id on tokens(user_id);
create index if not exists idx_permissions_repository_id on permissions(repository_id);

-- +goose Down
drop index if exists idx_ssh_keys_user_id;
drop index if exists idx_tokens_user_id;
drop index if exists idx_permissions_repository_id;

drop table if exists ssh_keys;
drop table if exists tokens;
drop table if exists permissions;