var stfuseApp = angular.module('syncthing-fuse', []);

stfuseApp.controller('SyncthingFuseController', function ($scope, $http) {
    $scope.config = {};

    $http.get('rest/system/config').success(function(data) {
        $scope.config = data;
    });

    $scope.thisDevice = function () {
        for (var i = 0; i < $scope.config.devices.length; i++) {
            var device = $scope.config.devices[i];
            if (device.deviceID === $scope.config.myID) {
                return device;
            }
        }
    };

    $scope.otherDevices = function() {
        var devices = [];

        for (var i = 0; i < $scope.config.devices.length; i++) {
            device = $scope.config.devices[i];
            if (device.deviceID !== $scope.config.myID) {
                devices.push(device)
            }
        }

        return devices;
    };
});

