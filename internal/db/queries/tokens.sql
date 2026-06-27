-- name: CreateToken :one
insert into tokens (
  user_id, title, token_hash, expires_at_unix_ms
) values (
  ?, ?, ?, ?
) returning *;

-- name: DeleteToken :exec
delete from tokens
where token_hash=?;

-- name: DeleteTokenByID :execrows
delete from tokens
where id=sqlc.arg(id) and user_id=sqlc.arg(user_id);

-- name: DeleteTokensByUserID :exec
delete from tokens
where user_id=?;

-- name: ListTokensByUser :many
select * from tokens
where user_id=?
order by created_at_unix_ms;

-- name: DeleteExpiredTokens :execrows
delete from tokens
where expires_at_unix_ms is not null
and expires_at_unix_ms <= cast(unixepoch('subsec') * 1000 as integer);

-- name: UpdateTokenUsedAt :exec
update tokens 
set last_used_at_unix_ms=CAST(unixepoch('subsec') * 1000 as integer)
where token_hash=sqlc.arg(token_hash);

-- name: GetUserByToken :one
select users.* from users
join tokens on tokens.user_id = users.id
where tokens.token_hash=sqlc.arg(token_hash)
and (tokens.expires_at_unix_ms is null or tokens.expires_at_unix_ms > cast(unixepoch('subsec') * 1000 as integer))
limit 1;