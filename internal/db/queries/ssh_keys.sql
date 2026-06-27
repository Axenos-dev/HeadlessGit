-- name: CreateSSHKey :one
insert into ssh_keys (
  user_id, title, public_key, fingerprint
) values (
  ?, ?, ?, ?
) returning *;

-- name: DeleteSSHKey :exec
delete from ssh_keys
where fingerprint=?;

-- name: DeleteSSHKeyByID :execrows
delete from ssh_keys
where id=sqlc.arg(id) and user_id=sqlc.arg(user_id);

-- name: ListSSHKeysByUser :many
select * from ssh_keys
where user_id=?
order by created_at_unix_ms;

-- name: UpdateSSHKeyUsedAt :exec
update ssh_keys 
set last_used_at_unix_ms=CAST(unixepoch('subsec') * 1000 as integer)
where fingerprint=sqlc.arg(fingerprint);

-- name: GetUserByFingerprint :one
select users.* from users
join ssh_keys on ssh_keys.user_id = users.id
where ssh_keys.fingerprint=sqlc.arg(fingerprint) limit 1;