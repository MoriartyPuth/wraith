import { PageHeader } from "../../components/ui";
import { BTDeviceList } from "../../components/bluetooth/BTDeviceList";

export default function ClassicDevices() {
  return (
    <>
      <PageHeader title="Bluetooth Classic"
        subtitle="BR/EDR devices that answered an inquiry. Only discoverable devices answer; the name is shown only if the device returned it." />
      <BTDeviceList transport="classic" />
      <p className="mt-3 text-xs text-ink-3">
        Classic inquiry is active: the sensor transmits inquiry packets while scanning. "Class" is decoded from the Class
        of Device the device itself advertises, never guessed from the address.
      </p>
    </>
  );
}
