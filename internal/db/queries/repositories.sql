-- name: GetRepository :one
select * from repositories
where id=? limit 1;

-- name: ListUserRepositories :many
select * from repositories 
where owner_id=?;

-- name: ListRepositories :many
select * from repositories;

-- name: CreateRepository :one
insert into repositories (
  owner_id, repository_name, storage_path, visibility
) values (
  ?, ?, ?, ?
) on conflict(owner_id, repository_name) do nothing
returning *;

-- name: UpdateRepositoryVisibility :one
update repositories
set visibility=sqlc.arg(visibility),
    updated_at_unix_ms=cast(unixepoch('subsec') * 1000 as integer)
where id=sqlc.arg(id)
returning *;

-- name: DeleteRepository :exec
delete from repositories
where id=?;

-- name: GetRepositoryByPath :one
select repositories.* from repositories
join users on users.id = repositories.owner_id
where users.username = sqlc.arg(namespace)
and repositories.repository_name = sqlc.arg(name)