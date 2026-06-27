-- name: GetPermission :one
select * from permissions
where user_id=? and repository_id=? limit 1;

-- name: UpsertPermission :one
insert into permissions (
  user_id, repository_id, user_role
) values (
  ?, ?, ?
) on conflict(user_id, repository_id) do update set user_role=excluded.user_role returning *;

-- name: ListRepositoryPermissions :many
select * from permissions
where repository_id=?
order by created_at_unix_ms;

-- name: DeletePermission :exec
delete from permissions
where user_id=? and repository_id=?;