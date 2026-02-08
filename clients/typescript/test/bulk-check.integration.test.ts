/**
 * Integration tests for BulkCheckBuilder.
 *
 * These tests run against a real PostgreSQL database with melange schema installed.
 */

import { describe, test, expect, beforeAll, afterAll } from 'vitest';
import { Pool } from 'pg';
import {
  Checker,
  MemoryCache,
  BulkCheckDeniedError,
  isBulkCheckDeniedError,
} from '../src/index.js';
import { createTestPool, verifyTestDatabase, closeTestPool } from './setup.js';

describe('BulkCheck Integration Tests', () => {
  let pool: Pool;
  let checker: Checker;

  // Shared test data IDs
  let ownerId: number;
  let memberId: number;
  let outsiderId: number;
  let orgId: number;
  let repo1Id: number;
  let repo2Id: number;
  let repo3Id: number;

  beforeAll(async () => {
    pool = createTestPool();
    await verifyTestDatabase(pool);
    checker = new Checker(pool);

    // Create users
    const ownerRes = await pool.query(
      "INSERT INTO users (username) VALUES ('ts_bulk_owner') RETURNING id",
    );
    ownerId = ownerRes.rows[0].id;

    const memberRes = await pool.query(
      "INSERT INTO users (username) VALUES ('ts_bulk_member') RETURNING id",
    );
    memberId = memberRes.rows[0].id;

    const outsiderRes = await pool.query(
      "INSERT INTO users (username) VALUES ('ts_bulk_outsider') RETURNING id",
    );
    outsiderId = outsiderRes.rows[0].id;

    // Create organization
    const orgRes = await pool.query(
      "INSERT INTO organizations (name) VALUES ('ts_bulk_org') RETURNING id",
    );
    orgId = orgRes.rows[0].id;

    // Add members
    await pool.query(
      'INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)',
      [orgId, ownerId, 'owner'],
    );
    await pool.query(
      'INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, $3)',
      [orgId, memberId, 'member'],
    );

    // Create repositories
    const repo1Res = await pool.query(
      'INSERT INTO repositories (name, organization_id) VALUES ($1, $2) RETURNING id',
      ['ts_bulk_repo1', orgId],
    );
    repo1Id = repo1Res.rows[0].id;

    const repo2Res = await pool.query(
      'INSERT INTO repositories (name, organization_id) VALUES ($1, $2) RETURNING id',
      ['ts_bulk_repo2', orgId],
    );
    repo2Id = repo2Res.rows[0].id;

    const repo3Res = await pool.query(
      'INSERT INTO repositories (name, organization_id) VALUES ($1, $2) RETURNING id',
      ['ts_bulk_repo3', orgId],
    );
    repo3Id = repo3Res.rows[0].id;
  });

  afterAll(async () => {
    await closeTestPool(pool);
  });

  test('mixed results: member allowed, outsider denied', async () => {
    const member = { type: 'user', id: String(memberId) };
    const outsider = { type: 'user', id: String(outsiderId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const results = await checker
      .newBulkCheck()
      .add(member, 'can_read', repo)
      .add(outsider, 'can_read', repo)
      .execute();

    expect(results.length).toBe(2);
    expect(results.get(0).allowed).toBe(true);
    expect(results.get(1).allowed).toBe(false);
    expect(results.all()).toBe(false);
    expect(results.any()).toBe(true);
    expect(results.allowed().length).toBe(1);
    expect(results.denied().length).toBe(1);
  });

  test('multiple relations in one batch', async () => {
    const owner = { type: 'user', id: String(ownerId) };
    const org = { type: 'organization', id: String(orgId) };

    const results = await checker
      .newBulkCheck()
      .add(owner, 'can_read', org)
      .add(owner, 'can_admin', org)
      .add(owner, 'can_delete', org)
      .execute();

    expect(results.length).toBe(3);
    // Owner should have all three
    expect(results.get(0).allowed).toBe(true);
    expect(results.get(1).allowed).toBe(true);
    expect(results.get(2).allowed).toBe(true);
    expect(results.all()).toBe(true);
  });

  test('multiple object types in one batch', async () => {
    const owner = { type: 'user', id: String(ownerId) };
    const org = { type: 'organization', id: String(orgId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const results = await checker
      .newBulkCheck()
      .add(owner, 'can_read', org)
      .add(owner, 'can_read', repo)
      .execute();

    expect(results.length).toBe(2);
    expect(results.get(0).allowed).toBe(true);
    expect(results.get(1).allowed).toBe(true);
  });

  test('addWithId with real SQL', async () => {
    const member = { type: 'user', id: String(memberId) };
    const outsider = { type: 'user', id: String(outsiderId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const results = await checker
      .newBulkCheck()
      .addWithId('member-read', member, 'can_read', repo)
      .addWithId('outsider-read', outsider, 'can_read', repo)
      .execute();

    const memberResult = results.getById('member-read');
    expect(memberResult).toBeDefined();
    expect(memberResult!.allowed).toBe(true);

    const outsiderResult = results.getById('outsider-read');
    expect(outsiderResult).toBeDefined();
    expect(outsiderResult!.allowed).toBe(false);
  });

  test('addMany across repos', async () => {
    const member = { type: 'user', id: String(memberId) };
    const r1 = { type: 'repository', id: String(repo1Id) };
    const r2 = { type: 'repository', id: String(repo2Id) };
    const r3 = { type: 'repository', id: String(repo3Id) };

    const results = await checker.newBulkCheck().addMany(member, 'can_read', r1, r2, r3).execute();

    expect(results.length).toBe(3);
    // Member of org should be able to read all repos in that org
    expect(results.all()).toBe(true);
  });

  test('deduplication: same check 3x, all results identical', async () => {
    const member = { type: 'user', id: String(memberId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const results = await checker
      .newBulkCheck()
      .add(member, 'can_read', repo)
      .add(member, 'can_read', repo)
      .add(member, 'can_read', repo)
      .execute();

    expect(results.length).toBe(3);
    expect(results.get(0).allowed).toBe(true);
    expect(results.get(1).allowed).toBe(true);
    expect(results.get(2).allowed).toBe(true);
  });

  test('parity with single check()', async () => {
    const member = { type: 'user', id: String(memberId) };
    const outsider = { type: 'user', id: String(outsiderId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    // Single checks
    const memberSingle = await checker.check(member, 'can_read', repo);
    const outsiderSingle = await checker.check(outsider, 'can_read', repo);

    // Bulk check
    const results = await checker
      .newBulkCheck()
      .add(member, 'can_read', repo)
      .add(outsider, 'can_read', repo)
      .execute();

    expect(results.get(0).allowed).toBe(memberSingle.allowed);
    expect(results.get(1).allowed).toBe(outsiderSingle.allowed);
  });

  test('allOrError with mixed results returns BulkCheckDeniedError', async () => {
    const member = { type: 'user', id: String(memberId) };
    const outsider = { type: 'user', id: String(outsiderId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const results = await checker
      .newBulkCheck()
      .add(member, 'can_read', repo)
      .add(outsider, 'can_read', repo)
      .execute();

    const err = results.allOrError();
    expect(err).toBeInstanceOf(BulkCheckDeniedError);
    expect(err!.index).toBe(1); // outsider is at index 1
    expect(err!.total).toBe(1);
    expect(isBulkCheckDeniedError(err)).toBe(true);
  });

  test('cache populated by bulk check', async () => {
    const cache = new MemoryCache(60000);
    const cachedChecker = new Checker(pool, { cache });

    const member = { type: 'user', id: String(memberId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    // Bulk check populates cache
    await cachedChecker.newBulkCheck().add(member, 'can_read', repo).execute();

    // Subsequent single check should use cached value
    const decision = await cachedChecker.check(member, 'can_read', repo);
    expect(decision.allowed).toBe(true);

    // Verify cache has the entry
    expect(cache.size).toBeGreaterThanOrEqual(1);
  });

  test('result ordering matches insertion order', async () => {
    const member = { type: 'user', id: String(memberId) };
    const r1 = { type: 'repository', id: String(repo1Id) };
    const r2 = { type: 'repository', id: String(repo2Id) };
    const r3 = { type: 'repository', id: String(repo3Id) };

    const results = await checker
      .newBulkCheck()
      .add(member, 'can_read', r1)
      .add(member, 'can_read', r2)
      .add(member, 'can_read', r3)
      .execute();

    expect(results.get(0).object.id).toBe(String(repo1Id));
    expect(results.get(1).object.id).toBe(String(repo2Id));
    expect(results.get(2).object.id).toBe(String(repo3Id));
  });

  test('single check in batch', async () => {
    const member = { type: 'user', id: String(memberId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const results = await checker.newBulkCheck().add(member, 'can_read', repo).execute();

    expect(results.length).toBe(1);
    expect(results.get(0).allowed).toBe(true);
  });

  test('large batch (100 checks)', async () => {
    const member = { type: 'user', id: String(memberId) };
    const outsider = { type: 'user', id: String(outsiderId) };
    const repo = { type: 'repository', id: String(repo1Id) };

    const builder = checker.newBulkCheck();
    for (let i = 0; i < 50; i++) {
      builder.add(member, 'can_read', repo);
    }
    for (let i = 0; i < 50; i++) {
      builder.add(outsider, 'can_read', repo);
    }

    const results = await builder.execute();

    expect(results.length).toBe(100);
    // First 50 should be allowed (member), last 50 denied (outsider)
    for (let i = 0; i < 50; i++) {
      expect(results.get(i).allowed).toBe(true);
    }
    for (let i = 50; i < 100; i++) {
      expect(results.get(i).allowed).toBe(false);
    }
  });
});
