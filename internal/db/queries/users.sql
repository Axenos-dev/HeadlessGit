-- name: GetUser :one
select * from users
where id=? limit 1;

-- name: CreateUser :one
insert into users (
  username, kind
) values (
  ?, ?
) returning *;