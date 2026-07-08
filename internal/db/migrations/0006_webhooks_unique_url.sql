-- +goose Up
-- drop duplicate registrations before adding the constraint
delete from webhooks
where id not in (
  select min(id) from webhooks
  group by repository_id, url
);

create unique index if not exists idx_webhooks_repository_id_url on webhooks(repository_id, url);

-- +goose Down
drop index if exists idx_webhooks_repository_id_url;
