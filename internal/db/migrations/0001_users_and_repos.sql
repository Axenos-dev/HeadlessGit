-- +goose Up
create table if not exists users (
  id integer primary key,
  username text not null unique,
  kind text not null check(kind in ('user', 'service')),

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),
  updated_at_unix_ms integer
);

create table if not exists repositories (
  id integer primary key,
  owner_id integer not null,
  repository_name text not null,
  storage_path text not null unique,

  visibility text not null default 'private' check(visibility in ('public','private')),

  created_at_unix_ms integer not null default (
    CAST(unixepoch('subsec') * 1000 as integer)
  ),
  updated_at_unix_ms integer,

  unique(owner_id, repository_name),
  foreign key (owner_id) references users(id)
);

-- +goose Down
drop table repositories;
drop table users;