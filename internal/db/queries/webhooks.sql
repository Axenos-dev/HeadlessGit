-- name: CreateWebhook :one
insert into webhooks (
  repository_id, secret, url
) values (
  ?, ?, ?
) on conflict(repository_id, url) do nothing
returning *;

-- name: ListWebhooksForRepository :many
select * from webhooks
where repository_id=?;

-- name: DeleteWebhook :exec
delete from webhooks
where id=? and repository_id=?;