import { generateBuildId, generateReleaseId } from '../../../src/db/id-generation';

describe('generateBuildId', () => {
  it('returns a string matching build-{8hex}', () => {
    const id = generateBuildId();
    expect(id).toMatch(/^build-[0-9a-f]{8}$/);
  });

  it('generates unique IDs across 1000 calls', () => {
    const ids = new Set<string>();
    for (let i = 0; i < 1000; i++) {
      ids.add(generateBuildId());
    }
    expect(ids.size).toBe(1000);
  });
});

describe('generateReleaseId', () => {
  it('returns a string matching rel-{8hex}', () => {
    const id = generateReleaseId();
    expect(id).toMatch(/^rel-[0-9a-f]{8}$/);
  });

  it('generates unique IDs across 1000 calls', () => {
    const ids = new Set<string>();
    for (let i = 0; i < 1000; i++) {
      ids.add(generateReleaseId());
    }
    expect(ids.size).toBe(1000);
  });
});
