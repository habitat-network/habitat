import type { ReactNode } from 'react';
import { Redirect } from '@docusaurus/router';
import useBaseUrl from '@docusaurus/useBaseUrl';

export default function Home(): ReactNode {
  return <Redirect to={useBaseUrl('/docs/habitat')} />;
}
