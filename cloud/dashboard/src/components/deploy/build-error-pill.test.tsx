import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { BuildErrorPill } from './build-error-pill';

describe('BuildErrorPill', () => {
  it('test_renders_class_label_for_known_class', () => {
    render(
      <MemoryRouter>
        <BuildErrorPill
          errorClass="OOM"
          errorMessage="Container exited"
          suggestion="Increase plan resources"
          projectName="teste"
          buildId="b-1"
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/out of memory/i)).toBeInTheDocument();
  });

  it('test_renders_suggestion_text', () => {
    render(
      <MemoryRouter>
        <BuildErrorPill
          errorClass="BUILD_TIMEOUT"
          errorMessage=""
          suggestion="Check stuck dependencies"
          projectName="teste"
          buildId="b-1"
        />
      </MemoryRouter>,
    );
    expect(screen.getByText(/check stuck dependencies/i)).toBeInTheDocument();
  });

  it('test_logs_link_points_to_logs_tab', () => {
    render(
      <MemoryRouter>
        <BuildErrorPill
          errorClass="OOM"
          errorMessage=""
          suggestion=""
          projectName="teste"
          buildId="b-1"
        />
      </MemoryRouter>,
    );
    const link = screen.getByRole('link', { name: /open build logs/i }) as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/projects/teste/deployments/b-1/logs');
  });

  it('test_unknown_class_falls_back_to_generic_label', () => {
    render(
      <MemoryRouter>
        <BuildErrorPill
          errorClass={'NOVEL_CLASS' as never}
          errorMessage=""
          suggestion=""
          projectName="teste"
          buildId="b-1"
        />
      </MemoryRouter>,
    );
    expect(screen.getByText(/build failed/i)).toBeInTheDocument();
  });
});
