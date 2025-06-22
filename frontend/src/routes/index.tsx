import { getWebApps } from '@/api/node';
import { createFileRoute, Link } from '@tanstack/react-router'

export const Route = createFileRoute('/')({
  async loader() {
    const webAppInstallations = await getWebApps();
    const filteredWebApps = webAppInstallations
      .filter((app: any) => app.driver === 'web')
      .map((app: any) => ({
        id: app.id,
        name: app.name,
        description: 'No description available',
        icon: '🌐', // Default icon for web apps
        link: app.url || '#'
      }));

    return [
      {
        id: 'my-server',
        name: 'My Server',
        description: 'Manage your server',
        icon: '🖥️',
        link: '/server'
      },
      {
        id: 'app-shop',
        name: 'App Gallery',
        description: 'Find apps to install on your server',
        icon: '🍎',
        link: '/app-store'
      },
      ...filteredWebApps
    ]

  },
  component() {
    const data = Route.useLoaderData()
    return (
      <>
        <h1>Apps</h1>
        <table>
          <thead>
            <tr>
              <th>App</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {data.map(({ id, name, description, icon, link }: any) => (
              <tr key={id}>
                <td>
                  <Link to={link}>{icon} {name}</Link>
                </td>
                <td>{description}</td>
              </tr>))}
          </tbody>
        </table>
      </>
    )
  },
})
