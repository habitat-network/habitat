import habitatLogo from "../images/habitat.svg";

export const HabitatLogo = ({ size = 32 }: { size?: number }) => {
  return <img src={habitatLogo} alt="Habitat" width={size} height={size} />;
};
