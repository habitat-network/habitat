import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';
import apiSidebar from './docs/api/sidebar';

const sidebars: SidebarsConfig = {
  docs: [
    {
      type: 'doc',
      id: 'getting-started',
    },
    {
      type: 'doc',
      id: 'concepts'
    },
    {
      type: 'category',
      label: 'HTTP Reference',
      items: [
        ...apiSidebar,
      ],
    }
  ]
};

export default sidebars;