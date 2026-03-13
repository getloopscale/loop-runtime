import { generateCorrelationId } from '../../../src/correlation/correlation-id';

const UUID_V4_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

describe('generateCorrelationId', () => {
  it('returns a valid UUID v4', () => {
    const id = generateCorrelationId();
    expect(id).toMatch(UUID_V4_REGEX);
  });

  it('returns unique values on each call', () => {
    const ids = new Set<string>();
    for (let i = 0; i < 1000; i++) {
      ids.add(generateCorrelationId());
    }
    expect(ids.size).toBe(1000);
  });
});
