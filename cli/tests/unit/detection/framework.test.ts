import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { detectFramework } from '../../../src/detection/framework';

describe('detectFramework', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-fw-'));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  function writeFile(relativePath: string, content: string): void {
    const fullPath = path.join(tmpDir, relativePath);
    fs.mkdirSync(path.dirname(fullPath), { recursive: true });
    fs.writeFileSync(fullPath, content);
  }

  describe('Next.js', () => {
    it('should detect by config file', () => {
      writeFile('next.config.js', 'module.exports = {}');
      writeFile('package.json', JSON.stringify({ dependencies: { next: '14.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('nextjs');
      expect(result.port).toBe(3000);
    });

    it('should detect by dependency when no config file', () => {
      writeFile('package.json', JSON.stringify({ dependencies: { next: '14.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('nextjs');
    });
  });

  describe('Vite', () => {
    it('should detect by config file', () => {
      writeFile('vite.config.ts', 'export default {}');
      writeFile('package.json', JSON.stringify({ dependencies: { vite: '5.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('vite');
      expect(result.port).toBe(5173);
    });

    it('should detect by dependency', () => {
      writeFile('package.json', JSON.stringify({ devDependencies: { vite: '5.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('vite');
    });
  });

  describe('Remix', () => {
    it('should detect by config file', () => {
      writeFile('remix.config.js', 'module.exports = {}');
      writeFile('package.json', JSON.stringify({ dependencies: { '@remix-run/node': '2.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('remix');
      expect(result.port).toBe(3000);
    });
  });

  describe('NestJS', () => {
    it('should detect by dependency', () => {
      writeFile('package.json', JSON.stringify({ dependencies: { '@nestjs/core': '10.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('nestjs');
      expect(result.port).toBe(3000);
    });
  });

  describe('Express', () => {
    it('should detect by dependency', () => {
      writeFile('package.json', JSON.stringify({ dependencies: { express: '4.18.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('express');
      expect(result.port).toBe(3000);
    });
  });

  describe('Fastify', () => {
    it('should detect by dependency', () => {
      writeFile('package.json', JSON.stringify({ dependencies: { fastify: '4.0.0' } }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('fastify');
      expect(result.port).toBe(3000);
    });
  });

  describe('FastAPI', () => {
    it('should detect by main.py with FastAPI()', () => {
      writeFile('main.py', 'from fastapi import FastAPI\napp = FastAPI()\n');

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('fastapi');
      expect(result.port).toBe(8000);
    });

    it('should detect by requirements.txt', () => {
      writeFile('requirements.txt', 'fastapi==0.100.0\nuvicorn');

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('fastapi');
    });
  });

  describe('Django', () => {
    it('should detect by manage.py', () => {
      writeFile('manage.py', '#!/usr/bin/env python\nimport django');

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('django');
      expect(result.port).toBe(8000);
    });
  });

  describe('Gin', () => {
    it('should detect by go.mod with gin dependency', () => {
      writeFile('go.mod', 'module myapp\n\nrequire github.com/gin-gonic/gin v1.9.0\n');

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('gin');
      expect(result.port).toBe(8080);
    });
  });

  describe('Node.js fallback', () => {
    it('should detect generic Node.js project', () => {
      writeFile('package.json', JSON.stringify({ name: 'my-app', main: 'index.js' }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('node');
      expect(result.port).toBe(3000);
    });
  });

  describe('custom/unknown', () => {
    it('should return custom for unknown project', () => {
      writeFile('Makefile', 'build:\n\tcc -o app main.c');

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('custom');
      expect(result.port).toBeNull();
    });

    it('should not throw on empty directory', () => {
      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('custom');
      expect(result.port).toBeNull();
    });
  });

  describe('priority', () => {
    it('should prefer config file over dependency', () => {
      writeFile('next.config.js', 'module.exports = {}');
      writeFile('package.json', JSON.stringify({
        dependencies: { next: '14.0.0', express: '4.18.0' },
      }));

      const result = detectFramework(tmpDir);
      expect(result.framework).toBe('nextjs');
    });
  });
});
