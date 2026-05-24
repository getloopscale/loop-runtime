import { describe, it, expect } from 'vitest';
import { render, screen } from '@/test/render';
import { DeviceSuccessPage } from './device-success';

describe('DeviceSuccessPage', () => {
  it('test_device_success_renders_success_state', () => {
    render(<DeviceSuccessPage />);
    expect(screen.getByText('CLI authorized')).toBeInTheDocument();
    expect(
      screen.getByText(/You can close this tab and return to your terminal/i),
    ).toBeInTheDocument();
  });
});
