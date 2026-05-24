import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useEffect } from 'react';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-projects', () => ({
  useProjects: () => ({ projects: [], loading: false }),
}));

import { TopBar } from './top-bar';
import { usePageMeta } from '@/hooks/use-page-meta';

function TitleSetter({ title }: { title: string }) {
  const { setPageTitle } = usePageMeta();
  // setPageTitle must run in an effect — calling it during render triggers
  // React's "setState during render" warning (state update on parent provider).
  useEffect(() => {
    setPageTitle(title);
  }, [setPageTitle, title]);
  return null;
}

describe('TopBar', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('test_top_bar_renders_project_selector_by_default', () => {
    render(<TopBar />);
    // Select primitive has aria-label="Select project"
    expect(screen.getByText('All Projects')).toBeInTheDocument();
  });

  it('test_top_bar_hides_project_selector_when_prop_false', () => {
    render(<TopBar showProjectSelector={false} />);
    expect(screen.queryByText('All Projects')).not.toBeInTheDocument();
  });

  it('test_top_bar_renders_page_title_when_provided', () => {
    // PageMetaProvider is already in test render wrapper; render setter + TopBar
    // to set + read the title.
    render(
      <>
        <TitleSetter title="My Test Page" />
        <TopBar />
      </>,
    );
    expect(screen.getByText('My Test Page')).toBeInTheDocument();
  });

  it('test_top_bar_hides_selector_on_settings_route', () => {
    render(<TopBar />, { initialEntries: ['/settings'] });
    expect(screen.queryByText('All Projects')).not.toBeInTheDocument();
  });

  it('test_top_bar_hides_selector_on_profile_and_billing', () => {
    const { unmount } = render(<TopBar />, { initialEntries: ['/profile'] });
    expect(screen.queryByText('All Projects')).not.toBeInTheDocument();
    unmount();
    render(<TopBar />, { initialEntries: ['/billing'] });
    expect(screen.queryByText('All Projects')).not.toBeInTheDocument();
  });
});
