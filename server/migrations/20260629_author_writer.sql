-- Author / writer field unification.
-- MySQL migration for the new Studio contract:
--   author = WeChat publish byline
--   writer = writing-style resource key

ALTER TABLE projects
  CHANGE COLUMN writing_style writer varchar(100) NOT NULL DEFAULT '',
  DROP COLUMN author_style_intro,
  DROP COLUMN author_avatar_url;

ALTER TABLE templates
  CHANGE COLUMN writing_style writer text,
  CHANGE COLUMN author_name author varchar(100),
  DROP COLUMN author_style_intro,
  DROP COLUMN author_avatar_url;

ALTER TABLE plans
  CHANGE COLUMN writer_key writer varchar(100) NOT NULL DEFAULT '',
  CHANGE COLUMN byline author varchar(200) NOT NULL DEFAULT '';

UPDATE tasks
SET overrides = JSON_SET(
  overrides,
  '$.writer',
  JSON_UNQUOTE(JSON_EXTRACT(overrides, '$.writer_key'))
)
WHERE overrides IS NOT NULL
  AND JSON_VALID(overrides)
  AND JSON_CONTAINS_PATH(overrides, 'one', '$.writer_key');

UPDATE tasks
SET overrides = JSON_SET(
  overrides,
  '$.author',
  JSON_UNQUOTE(JSON_EXTRACT(overrides, '$.byline'))
)
WHERE overrides IS NOT NULL
  AND JSON_VALID(overrides)
  AND JSON_CONTAINS_PATH(overrides, 'one', '$.byline');

UPDATE tasks
SET overrides = JSON_REMOVE(overrides, '$.writer_key', '$.byline', '$.writing_voice')
WHERE overrides IS NOT NULL
  AND JSON_VALID(overrides)
  AND (
    JSON_CONTAINS_PATH(overrides, 'one', '$.writer_key')
    OR JSON_CONTAINS_PATH(overrides, 'one', '$.byline')
    OR JSON_CONTAINS_PATH(overrides, 'one', '$.writing_voice')
  );
