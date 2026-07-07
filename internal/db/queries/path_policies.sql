-- name: CreatePathPolicy :one
insert into path_policies (
  repository_id, pattern, kind, reason
) values (
  ?, ?, ?, ?
) returning *;

-- name: DeletePathPolicy :exec
delete from path_policies
where id=? and repository_id=?;

-- name: ListRepositoryPathPolicies :many
select * from path_policies
where repository_id=?;