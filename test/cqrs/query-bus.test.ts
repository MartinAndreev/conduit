import { test } from "node:test";
import assert from "node:assert/strict";
import { QueryBus } from "../../src/system/bus/query-bus.js";
import type { Query, QueryHandler } from "../../src/system/bus/query-bus.js";

interface TestQuery extends Query {
  readonly type: "testQuery";
  readonly key: string;
}

interface TestReadModel {
  readonly name: string;
  readonly count: number;
}

test("QueryBus executes registered handler and returns read model", async () => {
  const bus = new QueryBus();
  const handler: QueryHandler<TestQuery, TestReadModel> = async (_q) => ({
    success: true,
    data: { name: "test", count: 42 },
  });
  bus.register<TestQuery, TestReadModel>("testQuery", handler);
  const result = await bus.execute<TestQuery, TestReadModel>({
    type: "testQuery",
    key: "a",
  });
  assert.equal(result.success, true);
  if (result.success) {
    assert.equal(result.data.name, "test");
    assert.equal(result.data.count, 42);
  }
});

test("QueryBus rejects duplicate handler registration", () => {
  const bus = new QueryBus();
  const handler: QueryHandler<TestQuery, TestReadModel> = async () => ({
    success: true,
    data: { name: "", count: 0 },
  });
  bus.register<TestQuery, TestReadModel>("testQuery", handler);
  assert.throws(
    () => bus.register<TestQuery, TestReadModel>("testQuery", handler),
    /Duplicate query handler registration: testQuery/,
  );
});

test("QueryBus returns error for unregistered query type", async () => {
  const bus = new QueryBus();
  const result = await bus.execute<TestQuery, TestReadModel>({
    type: "testQuery",
    key: "a",
  });
  assert.equal(result.success, false);
  if (!result.success) assert.equal(result.error.code, "HANDLER_NOT_FOUND");
});

test("QueryBus freezes returned result object", async () => {
  const bus = new QueryBus();
  bus.register<TestQuery, TestReadModel>("testQuery", async () => ({
    success: true,
    data: { name: "immutable", count: 1 },
  }));
  const result = await bus.execute<TestQuery, TestReadModel>({
    type: "testQuery",
    key: "a",
  });
  assert.equal(result.success, true);
  assert.equal(Object.isFrozen(result), true);
});

test("QueryBus catches handler exceptions", async () => {
  const bus = new QueryBus();
  bus.register<TestQuery, TestReadModel>("testQuery", async () => {
    throw new Error("query boom");
  });
  const result = await bus.execute<TestQuery, TestReadModel>({
    type: "testQuery",
    key: "a",
  });
  assert.equal(result.success, false);
  if (!result.success) {
    assert.equal(result.error.code, "HANDLER_ERROR");
    assert.match(result.error.message, /query boom/);
  }
});

test("QueryBus handles non-Error thrown values", async () => {
  const bus = new QueryBus();
  bus.register<TestQuery, TestReadModel>("testQuery", async () => {
    throw "string error";
  });
  const result = await bus.execute<TestQuery, TestReadModel>({
    type: "testQuery",
    key: "a",
  });
  assert.equal(result.success, false);
  if (!result.success) assert.equal(result.error.code, "HANDLER_ERROR");
});
