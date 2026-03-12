import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';
import apiSidebar from './docs/api/sidebar';

const sidebars: SidebarsConfig = {
  openApiSidebar: [
    ...apiSidebar,
  ],
};

export default sidebars;