CREATE SEQUENCE IF NOT EXISTS workbench_office_node_revision_seq AS BIGINT;

LOCK TABLE workbench_office_nodes IN ACCESS EXCLUSIVE MODE;

SELECT setval(
    'workbench_office_node_revision_seq',
    (SELECT GREATEST(COALESCE(MAX(revision), 0), 1) FROM workbench_office_nodes)
);

ALTER TABLE workbench_office_nodes
    ALTER COLUMN revision SET DEFAULT nextval('workbench_office_node_revision_seq');
