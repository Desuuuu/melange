/**
 * Unit tests for BulkCheckBuilder, BulkCheckResult, and BulkCheckResults.
 *
 * All tests use decision overrides so no database is needed.
 */

import { describe, test, expect } from 'vitest';
import { Checker, DecisionAllow, DecisionDeny } from './checker.js';
import { BulkCheckDeniedError, isBulkCheckDeniedError } from './errors.js';
import { MAX_BULK_CHECK_SIZE } from './bulk-check.js';

// Helpers — null db is fine because decision overrides bypass SQL.
const allowChecker = new Checker(null as any, { decision: DecisionAllow });
const denyChecker = new Checker(null as any, { decision: DecisionDeny });

const user = { type: 'user', id: '1' };
const repo = (id: string) => ({ type: 'repository', id });

describe('BulkCheckBuilder', () => {
  test('add() assigns sequential auto-IDs', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .add(user, 'can_read', repo('2'))
      .add(user, 'can_read', repo('3'))
      .execute();

    expect(results.length).toBe(3);
    expect(results.get(0).id).toBe('0');
    expect(results.get(1).id).toBe('1');
    expect(results.get(2).id).toBe('2');
  });

  test('addWithId() stores custom IDs', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .addWithId('alpha', user, 'can_read', repo('10'))
      .addWithId('beta', user, 'can_read', repo('20'))
      .execute();

    const alpha = results.getById('alpha');
    expect(alpha).toBeDefined();
    expect(alpha!.object.id).toBe('10');

    const beta = results.getById('beta');
    expect(beta).toBeDefined();
    expect(beta!.object.id).toBe('20');
  });

  test('addWithId() throws on empty ID', () => {
    const builder = allowChecker.newBulkCheck();
    expect(() => builder.addWithId('', user, 'can_read', repo('1'))).toThrow(
      'id must not be empty',
    );
  });

  test('addWithId() throws on duplicate ID', () => {
    const builder = allowChecker.newBulkCheck();
    builder.addWithId('dup', user, 'can_read', repo('1'));
    expect(() => builder.addWithId('dup', user, 'can_read', repo('2'))).toThrow(
      'duplicate id "dup"',
    );
  });

  test('addMany() adds multiple objects', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .addMany(user, 'can_read', repo('1'), repo('2'), repo('3'))
      .execute();

    expect(results.length).toBe(3);
    expect(results.get(0).object.id).toBe('1');
    expect(results.get(1).object.id).toBe('2');
    expect(results.get(2).object.id).toBe('3');
  });

  test('chaining returns builder', () => {
    const builder = allowChecker.newBulkCheck();
    const result = builder
      .add(user, 'can_read', repo('1'))
      .addWithId('custom', user, 'can_read', repo('2'))
      .addMany(user, 'can_read', repo('3'));

    expect(result).toBe(builder);
  });
});

describe('BulkCheckResults — DecisionAllow', () => {
  test('all results allowed', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .add(user, 'can_read', repo('2'))
      .add(user, 'can_read', repo('3'))
      .execute();

    expect(results.all()).toBe(true);
    expect(results.none()).toBe(false);
    expect(results.any()).toBe(true);
    expect(results.allowed().length).toBe(3);
    expect(results.denied().length).toBe(0);
  });
});

describe('BulkCheckResults — DecisionDeny', () => {
  test('all results denied', async () => {
    const results = await denyChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .add(user, 'can_read', repo('2'))
      .add(user, 'can_read', repo('3'))
      .execute();

    expect(results.all()).toBe(false);
    expect(results.none()).toBe(true);
    expect(results.any()).toBe(false);
    expect(results.allowed().length).toBe(0);
    expect(results.denied().length).toBe(3);
  });
});

describe('BulkCheckResults — empty batch', () => {
  test('empty batch returns correct aggregates', async () => {
    const results = await allowChecker.newBulkCheck().execute();

    expect(results.length).toBe(0);
    expect(results.all()).toBe(false);
    expect(results.none()).toBe(true);
    expect(results.any()).toBe(false);
    expect(results.allOrError()).toBeNull();
  });
});

describe('BulkCheckResult accessors', () => {
  test('result fields are correct', async () => {
    const subject = { type: 'user', id: '42' };
    const object = { type: 'repository', id: '99' };

    const results = await allowChecker
      .newBulkCheck()
      .addWithId('check-1', subject, 'can_write', object)
      .execute();

    const r = results.get(0);
    expect(r.id).toBe('check-1');
    expect(r.index).toBe(0);
    expect(r.subject.type).toBe('user');
    expect(r.subject.id).toBe('42');
    expect(r.relation).toBe('can_write');
    expect(r.object.type).toBe('repository');
    expect(r.object.id).toBe('99');
    expect(r.allowed).toBe(true);
    expect(r.error).toBeUndefined();
  });
});

describe('BulkCheckResults.getById', () => {
  test('returns undefined for unknown ID', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .execute();

    expect(results.getById('nonexistent')).toBeUndefined();
  });
});

describe('BulkCheckResults.get', () => {
  test('throws on out-of-range index', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .execute();

    expect(() => results.get(5)).toThrow(RangeError);
    expect(() => results.get(-1)).toThrow(RangeError);
  });
});

describe('BulkCheckResults.allOrError', () => {
  test('returns null when all allowed', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .add(user, 'can_read', repo('2'))
      .execute();

    expect(results.allOrError()).toBeNull();
  });

  test('returns BulkCheckDeniedError when denied', async () => {
    const results = await denyChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .add(user, 'can_read', repo('2'))
      .add(user, 'can_read', repo('3'))
      .execute();

    const err = results.allOrError();
    expect(err).toBeInstanceOf(BulkCheckDeniedError);
    expect(err!.total).toBe(3);
    expect(err!.index).toBe(0);
    expect(err!.subject.type).toBe('user');
    expect(err!.relation).toBe('can_read');
    expect(isBulkCheckDeniedError(err)).toBe(true);
  });
});

describe('allowed() and denied() filtering', () => {
  test('results() returns all results in order', async () => {
    const results = await allowChecker
      .newBulkCheck()
      .add(user, 'can_read', repo('1'))
      .add(user, 'can_read', repo('2'))
      .execute();

    const all = results.results();
    expect(all.length).toBe(2);
    expect(all[0].object.id).toBe('1');
    expect(all[1].object.id).toBe('2');
  });
});

describe('MAX_BULK_CHECK_SIZE', () => {
  test('equals 10000', () => {
    expect(MAX_BULK_CHECK_SIZE).toBe(10000);
  });
});
