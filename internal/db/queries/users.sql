-- name: GetUser :one
select * from users
where id=? limit 1;

-- name: CreateUser :one
insert into users (
  username, kind
) values (
  ?, ?
) on conflict(username) do nothing
returning *;

-- name: EnsureAdminUser :one
insert into users (
  username, kind, is_admin
) values (
  'admin', 'service', 1
) on conflict(username) do update set is_admin=1
returning *;